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

package syncmachineset

import (
	"bytes"
	"fmt"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	coapi "github.com/openshift/cluster-operator/pkg/api"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	"github.com/openshift/cluster-operator/pkg/logging"

	clusterapiv1 "k8s.io/kube-deploy/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclient "k8s.io/kube-deploy/cluster-api/pkg/client/clientset_generated/clientset"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a machineset.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	controllerLogName = "syncmachineset"

	remoteClusterAPINamespace      = "kube-cluster"
	errorMsgClusterAPINotInstalled = "cannot sync until cluster API is installed"
)

// NewController returns a new *Controller.
func NewController(
	machineSetInformer informers.MachineSetInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient coclient.Interface,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_nodeconfig_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)

	c := &Controller{
		client:     clusteroperatorClient,
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "syncmachineset"),
		logger:     logger,
	}

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		// TODO: delete
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	c.syncHandler = c.syncMachineSet
	c.enqueueMachineSet = c.enqueue
	c.buildClients = c.buildRemoteClusterClients

	return c
}

// Controller manages launching node config daemonset setup on machines
// that are masters in the cluster.
type Controller struct {
	client     coclient.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncMachineSet for testing.
	syncHandler func(queuedMS queuedMachineSet) error

	// used for unit testing
	enqueueMachineSet func(machineSet *cov1.MachineSet)

	buildClients func(cluster *cov1.MachineSet) (clusterapiclient.Interface, error)

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

// queuedMachineSet represents data we need to process a machine set in the queue. This includes
// data we might need to process deleted objects we can no longer lookup.
type queuedMachineSet struct {
	key         string
	clusterName string
}

func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(*cov1.MachineSet)
	if !isMasterMachineSet(machineSet) {
		return
	}
	logging.WithMachineSet(c.logger, machineSet).Debugf("Adding master machine set")
	c.enqueueMachineSet(machineSet)
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	machineSet := cur.(*cov1.MachineSet)
	if !isMasterMachineSet(machineSet) {
		return
	}
	logging.WithMachineSet(c.logger, machineSet).Debugf("Updating master machine set")
	c.enqueueMachineSet(machineSet)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machine sets will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("Starting syncmachineset controller")
	defer c.logger.Infof("Shutting down syncmachineset controller")

	if !controller.WaitForCacheSync("syncmachineset", stopCh, c.machineSetsSynced) {
		c.logger.Errorf("Could not sync caches for syncmachineset controller")
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(machineSet *cov1.MachineSet) {
	key, err := controller.KeyFunc(machineSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machineSet, err))
		return
	}

	c.queue.Add(queuedMachineSet{
		key:         key,
		clusterName: machineSet.Labels[controller.ClusterNameLabel],
	})
}

// enqueueAfter will enqueue a machine set after the provided amount of time.
func (c *Controller) enqueueAfter(ms *cov1.MachineSet, after time.Duration) {
	key, err := controller.KeyFunc(ms)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", ms, err))
		return
	}

	clusterName, ok := ms.Labels[controller.ClusterNameLabel]
	if !ok {
		utilruntime.HandleError(fmt.Errorf("couldn't get cluster name for machineset: %#v", ms))
	}

	c.queue.AddAfter(queuedMachineSet{
		key:         key,
		clusterName: clusterName,
	}, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	queuedMS, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(queuedMS)

	err := c.syncHandler(queuedMS.(queuedMachineSet))
	c.handleErr(err, queuedMS)

	return true
}

func (c *Controller) handleErr(err error, queuedMS interface{}) {
	if err == nil {
		c.queue.Forget(queuedMS)
		return
	}

	logger := c.logger.WithField("machineset", queuedMS)

	if c.queue.NumRequeues(queuedMS) < maxRetries {
		logger.Infof("Error syncing master machine set: %v", err)
		c.queue.AddRateLimited(queuedMS)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("Dropping master machine set out of the queue: %v", err)
	c.queue.Forget(queuedMS)
}

func isMasterMachineSet(machineSet *cov1.MachineSet) bool {
	return machineSet.Spec.NodeType == cov1.NodeTypeMaster
}

func (c *Controller) buildRemoteClusterClients(machineSet *cov1.MachineSet) (clusterapiclient.Interface, error) {
	// Load the kubeconfig secret for communicating with the remote cluster:
	// TODO: once implemented
	return nil, nil

}

// syncMachineSet will sync the machine set with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *Controller) syncMachineSet(queuedMS queuedMachineSet) error {
	startTime := time.Now()
	key := queuedMS.key
	c.logger.WithField("key", key).Debug("syncing machineset")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing machineset")
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	machineSet, err := c.machineSetsLister.MachineSets(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Debug("machineset has been deleted")
		// TODO: ensure deleted in remote cluster. If so, how are we going to figure out what cluster
		// the machine set was from to load the correct kubeconfig secret?
		return nil
	}
	if err != nil {
		return err
	}

	if machineSet.Spec.NodeType == cov1.NodeTypeMaster {
		return c.syncMasterMachineSet(machineSet)
	} //else {
	// TODO:
	//c.syncComputeMachineSet(machineSet)
	//}
	return nil
}

func (c *Controller) syncMasterMachineSet(ms *cov1.MachineSet) error {
	msLog := logging.WithMachineSet(c.logger, ms)
	msLog.Debug("loaded machineset")

	if !ms.Status.ClusterAPIInstalled {
		msLog.Debugf(errorMsgClusterAPINotInstalled)
		return fmt.Errorf(errorMsgClusterAPINotInstalled)
	}

	remoteClusterAPIClient, err := c.buildClients(ms)
	if err != nil {
		return err
	}

	// Lookup the CO cluster object for this machine set:
	coCluster, err := c.client.ClusteroperatorV1alpha1().Clusters(ms.Namespace).Get(ms.Labels[controller.ClusterNameLabel], metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error looking up machineset's cluster: %v", err)
	}
	msLog = logging.WithCluster(msLog, coCluster)

	// Create the Cluster object if it does not already exist:
	rCluster, err := remoteClusterAPIClient.ClusterV1alpha1().Clusters(remoteClusterAPINamespace).Get(coCluster.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Cluster does not exist, create it:
		msLog.Infof("creating new cluster API object in remote cluster")
		rCluster = buildClusterAPICluster(coCluster)
		_, err := remoteClusterAPIClient.ClusterV1alpha1().Clusters(remoteClusterAPINamespace).Create(rCluster)
		if err != nil {
			return fmt.Errorf("error creating cluster object in remote cluster: %v", err)
		}
	} else if err != nil {
		return err
	} else {
		// TODO: patch/update here?
		msLog.Debugf("cluster API object already exists in remote cluster")
	}

	/*

		// Check if this machine set exists in the remote cluster, create if not. We assume
		// the same name, and the "kube-cluster" namespace.
		remoteMachineSet, err := remoteClusterAPIClient.ClusterV1alpha1().MachineSets(remoteClusterAPINamespace).Get(ms.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// Machine set does not exist in remote cluster, create it:
		} else if err == nil {
			// Machine set exists in remote cluster, see if we need to update it:
		}
	*/

	return nil
}

func buildClusterAPICluster(cluster *cov1.Cluster) *clusterapiv1.Cluster {
	capiCluster := &clusterapiv1.Cluster{}
	capiCluster.Name = cluster.Name
	capiCluster.Spec.ProviderConfig = serializeCOResource(cluster)
	return capiCluster
}

func serializeCOResource(object runtime.Object) string {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, coapi.Scheme, coapi.Scheme, false)
	encoder := coapi.Codecs.EncoderForVersion(serializer, cov1.SchemeGroupVersion)
	buffer := &bytes.Buffer{}
	encoder.Encode(object, buffer)
	return buffer.String()
}
