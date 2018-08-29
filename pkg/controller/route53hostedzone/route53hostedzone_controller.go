/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package route53hostedzone

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	coclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws"
)

const (
	controllerName     = "route53hostedzone"
	zoneResyncDuration = 2 * time.Hour
)

// Controller manages route53 hosted zone object creation and updating
type Controller struct {
	// dnsZoneLister gives cached access to dnsZones objects in kube.
	dnsZoneLister  lister.DNSZoneLister
	dnsZonesSynced cache.InformerSynced

	// queue is where incoming work is placed to de-dup and to allow "easy"
	// rate limited requeues on errors
	queue workqueue.RateLimitingInterface

	// clusteroperatorClient is a kubernetes client to access cluster operator related objects.
	clusteroperatorClient coclient.Interface

	// kubeClient is a kubernetes client to access general cluster / project related objects.
	kubeClient kubeclientset.Interface

	logger log.FieldLogger

	// awsClientBuilder is a function pointer to the function that builds the cluster operator aws client.
	awsClientBuilder func(kubeClient kubeclientset.Interface, secretName, namespace, region string) (clustopaws.Client, error)
}

// NewController returns a new *Controller that is ready to reconcile dnszone objects.
func NewController(
	dnsZoneInformer informers.DNSZoneInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient coclient.Interface,
) *Controller {

	c := &Controller{
		dnsZoneLister:         dnsZoneInformer.Lister(),
		dnsZonesSynced:        dnsZoneInformer.Informer().HasSynced,
		queue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName),
		logger:                log.WithField("controller", controllerName),
		clusteroperatorClient: clusteroperatorClient,
		kubeClient:            kubeClient,
		awsClientBuilder:      clustopaws.NewClient,
	}

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_route53hostedzone_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	// register event handlers to fill the queue with DNSZone creations, updates and deletions
	dnsZoneInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				c.queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta nodeQueue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
	})

	return c
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many DNSZones will be handled in parallel.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	// don't let panics crash the process
	defer utilruntime.HandleCrash()
	// make sure the work queue is shutdown which will trigger workers to end
	defer c.queue.ShutDown()

	c.logger.Infof("Starting %s controller", controllerName)

	// wait for your secondary caches to fill before starting your work
	if !cache.WaitForCacheSync(stopCh, c.dnsZonesSynced) {
		return
	}

	// start up your worker threads based on threadiness.  Some controllers
	// have multiple kinds of workers
	for i := 0; i < threadiness; i++ {
		// runWorker will loop until "something bad" happens.  The .Until will
		// then rekick the worker after one second
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	// wait until we're told to stop
	<-stopCh
	c.logger.Infof("Shutting down %s controller", controllerName)
}

func (c *Controller) runWorker() {
	// hot loop until we're told to stop.  processNextWorkItem will
	// automatically wait until there's work available, so we don't worry
	// about secondary waits
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem deals with one key off the queue.  It returns false
// when it's time to quit.
func (c *Controller) processNextWorkItem() bool {
	// pull the next work item from queue.  It should be a key we use to lookup
	// something in a cache
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	// you always have to indicate to the queue that you've completed a piece of
	// work
	defer c.queue.Done(key)

	// do your work on the key.  This method will contains your "do stuff" logic
	err := c.syncHandler(key.(string))
	if err == nil {
		// if you had no error, tell the queue to stop tracking history for your
		// key. This will reset things like failure counts for per-item rate
		// limiting
		c.queue.Forget(key)
		return true
	}

	// there was a failure so be sure to report it.  This method allows for
	// pluggable error handling which can be used for things like
	// cluster-monitoring
	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", key, err))

	// since we failed, we should requeue the item to work on later.  This
	// method will add a backoff to avoid hotlooping on particular items
	// (they're probably still not going to work right away) and overall
	// controller protection (everything I've done is broken, this controller
	// needs to calm down or it can starve other useful work) cases.
	c.queue.AddRateLimited(key)

	return true
}

// syncHandler ensures that the objects in kube are reconciled properly.
func (c *Controller) syncHandler(key string) error {
	c.logger.Infof("Starting route53hostedzone sync for: %v", key)
	defer c.logger.Infof("End route53hostedzone sync for: %v", key)

	desiredState, err := c.getDesiredState(key)
	if errors.IsNotFound(err) {
		// IsNotFound is acceptable here. It just means that the object was likely deleted. No sync needed.
		c.logger.WithField("key", key).Info("DNSZone has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	clusterOperatorAWSClient, err := c.getCOAWSClient(desiredState)
	if err != nil {
		return err
	}

	// See if we need to sync. This is what rate limits our AWS API usage, but allows for immediate syncing on spec changes and deletes.
	shouldSync, delta := shouldSync(desiredState)
	if !shouldSync {
		c.logger.WithFields(log.Fields{
			"key":                  key,
			"delta":                delta,
			"currentGeneration":    desiredState.Generation,
			"lastSyncedGeneration": desiredState.Status.LastSyncGeneration,
		}).Debug("Sync not needed")

		return nil
	}

	zr, err := NewZoneReconciler(
		desiredState,
		c.kubeClient,
		c.clusteroperatorClient,
		c.logger,
		clusterOperatorAWSClient,
	)
	if err != nil {
		return err
	}

	// Actually reconcile desired state with current state.
	c.logger.WithFields(log.Fields{
		"key":                key,
		"delta":              delta,
		"currentGeneration":  desiredState.Generation,
		"lastSyncGeneration": desiredState.Status.LastSyncGeneration,
	}).Infof("Syncing DNS Zone: %v", desiredState.Spec.Zone)
	err = zr.Reconcile()
	return err
}

// shouldSync determines if the controller needs to sync the desiredState.
// This is what rate limits our AWS API usage, but allows for immediate syncing on spec changes and deletes.
//
// Rules:
// * Sync if we're in a deleting state (DeletionTimestamp != nil)
// * Sync if we've never sync'd before (LastSyncTimestamp == nil)
// * Sync if the last generation sync is not the same as the current generation
// * Sync if it's been zoneResyncDuration amount of time since the last sync
func shouldSync(desiredState *cov1.DNSZone) (bool, time.Duration) {
	if desiredState.DeletionTimestamp != nil {
		return true, 0 // We're in a deleting state, sync now.
	}

	if desiredState.Status.LastSyncTimestamp == nil {
		return true, 0 // We've never sync'd before, sync now.
	}

	if desiredState.Status.LastSyncGeneration != desiredState.Generation {
		return true, 0 // Spec has changed since last sync, sync now.
	}

	delta := time.Now().Sub(desiredState.Status.LastSyncTimestamp.Time)
	if delta >= zoneResyncDuration {
		// We haven't sync'd in over zoneResyncDuration time, sync now.
		return true, delta
	}

	// We didn't meet any of the criteria above, so we should not sync.
	return false, delta
}

// getDesiredState gets the current desired state from kubernetes.
func (c *Controller) getDesiredState(key string) (*cov1.DNSZone, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}

	dnszone, err := c.dnsZoneLister.DNSZones(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	return dnszone.DeepCopy(), nil
}

// getCOAWSClient generates a cluster operator aws client.
func (c *Controller) getCOAWSClient(dnsZone *cov1.DNSZone) (clustopaws.Client, error) {
	// This allows for using host profiles for AWS auth.
	var secretName, regionName string

	if dnsZone != nil && dnsZone.Spec.AWS != nil {
		secretName = dnsZone.Spec.AWS.AccountSecret.Name
		regionName = dnsZone.Spec.AWS.Region
	}

	coawsclient, err := c.awsClientBuilder(c.kubeClient, secretName, dnsZone.Namespace, regionName)
	if err != nil {
		c.logger.Errorf("Error creating COAWSClient: %v", err)
		return nil, err
	}

	return coawsclient, nil
}
