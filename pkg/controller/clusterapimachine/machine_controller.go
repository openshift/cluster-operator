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

package clusterapimachine

import (
	"fmt"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	clustopv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	clustoplisters "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	clustoplog "github.com/openshift/cluster-operator/pkg/logging"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	capilisters "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"
)

const (
	controllerLogName = "capi-machine"
)

// NewController returns a new controller.
func NewController(
	clusterInformer capiinformers.ClusterInformer,
	msInformer capiinformers.MachineInformer,
	cvInformer clustopinformers.ClusterVersionInformer,
	kubeClient kubeclientset.Interface,
	capiClient capiclientset.Interface) *Controller {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_capi_machine_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)
	c := &Controller{
		capiClient: capiClient,
		cvLister:   cvInformer.Lister(),
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "capi-machine"),
		logger:     logger,
	}
	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	msInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachine,
		UpdateFunc: c.updateMachine,
		DeleteFunc: c.deleteMachine,
	})
	c.machinesLister = msInformer.Lister()
	c.machinesSynced = msInformer.Informer().HasSynced

	c.syncHandler = c.syncMachine
	c.enqueueMachine = c.enqueue

	return c
}

// Controller manages populating Machines with data needed for the actuator.
type Controller struct {
	capiClient capiclientset.Interface

	// To allow injection of syncMachine for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueMachine func(cluster *capiv1.Machine)

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewController.
	clustersLister capilisters.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// machinesLister is able to list/get machines and is populated by the shared informer passed to
	// NewController.
	machinesLister capilisters.MachineLister
	// machinesSynced returns true if the machineset shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machinesSynced cache.InformerSynced

	// cvLister is able to list/get cluster versions and is populated by the shared infromer passed
	// to NewController.
	cvLister clustoplisters.ClusterVersionLister

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addMachine(obj interface{}) {
	m := obj.(*capiv1.Machine)
	clustoplog.WithClusterAPIMachine(c.logger, m).Debugf("adding machine")
	c.enqueueMachine(m)
}

func (c *Controller) updateMachine(old, cur interface{}) {
	oldM := old.(*capiv1.Machine)
	newM := cur.(*capiv1.Machine)
	clustoplog.WithClusterAPIMachine(c.logger, oldM).Debugf("updating machine")
	c.enqueueMachine(newM)
}

func (c *Controller) deleteMachine(obj interface{}) {
	m, ok := obj.(*capiv1.Machine)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		m, ok = tombstone.Obj.(*capiv1.Machine)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Machine %#v", obj))
			return
		}
	}
	clustoplog.WithClusterAPIMachine(c.logger, m).Debugf("deleting machine")
	c.enqueueMachine(m)
}

// enqueueMachinesForCluster looks up all machines associated with the given cluster and
// requeues them. Today this uses our own label convention, hopefully this will be a supported
// upstream convention soon.
func (c *Controller) enqueueMachinesForCluster(cluster *capiv1.Cluster) {
	nsMachines, err := c.machinesLister.Machines(cluster.Namespace).List(labels.SelectorFromSet(
		labels.Set{clustopv1.ClusterNameLabel: cluster.Name}))
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	for _, m := range nsMachines {
		clustoplog.WithClusterAPIMachine(c.logger, m).Debug("requeueing machine for cluster")
		c.enqueueMachine(m)
	}
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*capiv1.Cluster)
	clustoplog.WithClusterAPICluster(c.logger, cluster).Debugf("adding cluster")
	c.enqueueMachinesForCluster(cluster)
}

func (c *Controller) updateCluster(old, cur interface{}) {
	oldC := old.(*capiv1.Cluster)
	newC := cur.(*capiv1.Cluster)
	clustoplog.WithClusterAPICluster(c.logger, oldC).Debugf("updating cluster")
	c.enqueueMachinesForCluster(newC)
}

func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*capiv1.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*capiv1.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	clustoplog.WithClusterAPICluster(c.logger, cluster).Debugf("deleting cluster")
	c.enqueueMachinesForCluster(cluster)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machines will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("starting %s controller", controllerLogName)
	defer c.logger.Infof("shutting down %s controller", controllerLogName)

	if !controller.WaitForCacheSync("capi-machine", stopCh, c.machinesSynced, c.clustersSynced) {
		c.logger.Info("waiting for caches to sync")
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(ms *capiv1.Machine) {
	key, err := controller.KeyFunc(ms)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", ms, err))
		return
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	if err == nil {
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("Sync %q failed with %v", key, err))
	c.queue.AddRateLimited(key)

	return true
}

// syncMachine will sync the machine with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *Controller) syncMachine(key string) error {
	startTime := time.Now()
	c.logger.WithField("key", key).Debug("syncing machine")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing machine")
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	machine, err := c.machinesLister.Machines(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Debug("machine has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	mLog := clustoplog.WithClusterAPIMachine(c.logger, machine)
	mLog.Debugf("found machine")

	// Lookup the cluster for this machine:
	clusterName, ok := machine.Labels[clustopv1.ClusterNameLabel]
	if !ok {
		return fmt.Errorf("no cluster label set on machine")
	}
	cluster, err := c.clustersLister.Clusters(namespace).Get(clusterName)
	if errors.IsNotFound(err) {
		mLog.WithField("cluster", clusterName).Errorf("no cluster found for machine")
		return nil
	}
	if err != nil {
		return err
	}

	clusterSpec, err := controller.ClusterSpecFromClusterAPI(cluster)
	if err != nil {
		return err
	}

	// TODO: once cluster controller is setting resolved cluster version refs on the
	// ClusterStatus, replace this with the resolved ref not a lookup
	clusterVersion, err := c.cvLister.ClusterVersions(clusterSpec.ClusterVersionRef.Namespace).Get(clusterSpec.ClusterVersionRef.Name)
	if err != nil {
		mLog.Errorf("error looking up cluster version: %v", clusterSpec.ClusterVersionRef)
		return err
	}
	/*
		clusterStatus, err := controller.ClusterStatusFromClusterAPI(cluster)
		if err != nil {
			return err
		}
	*/

	newMachine := machine.DeepCopy()
	err = controller.PopulateMachineSpec(&newMachine.Spec, clusterSpec, clusterVersion, mLog)
	if err != nil {
		mLog.Errorf("error populating machine spec: %v", err)
		return err
	}

	if !apiequality.Semantic.DeepEqual(machine, newMachine) {
		_, err = c.capiClient.ClusterV1alpha1().Machines(cluster.Namespace).Update(machine)
		if err != nil {
			return err
		}
		mLog.Debug("machine updated")
	} else {
		mLog.Debug("no change to machine")
	}

	return nil
}
