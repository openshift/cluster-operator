/*
Copyright 2017 The Kubernetes Authors.

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

package machineset

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

// NewMachineSetController returns a new *MachineSetController.
func NewMachineSetController(machineSetInformer informers.MachineSetInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *MachineSetController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_node_group_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &MachineSetController{
		client: clusteroperatorClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineSet"),
	}

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		DeleteFunc: c.deleteMachineSet,
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	c.syncHandler = c.syncMachineSet
	c.enqueueMachineSet = c.enqueue

	return c
}

// MachineSetController manages provisioning node groups.
type MachineSetController struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncMachineSet for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueMachineSet func(machineSet *clusteroperator.MachineSet)

	// machineSetsLister is able to list/get node groups and is populated by the shared informer passed to
	// NewMachineSetController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the node group shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// MachineSets that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *MachineSetController) addMachineSet(obj interface{}) {
	h := obj.(*clusteroperator.MachineSet)
	glog.V(4).Infof("Adding node group %s", h.Name)
	c.enqueueMachineSet(h)
}

func (c *MachineSetController) updateMachineSet(old, cur interface{}) {
	oldNg := old.(*clusteroperator.MachineSet)
	curNg := cur.(*clusteroperator.MachineSet)
	glog.V(4).Infof("Updating node group %s", oldNg.Name)
	c.enqueueMachineSet(curNg)
}

func (c *MachineSetController) deleteMachineSet(obj interface{}) {
	ng, ok := obj.(*clusteroperator.MachineSet)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		ng, ok = tombstone.Obj.(*clusteroperator.MachineSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a MachineSet %#v", obj))
			return
		}
	}
	glog.V(4).Infof("Deleting node group %s", ng.Name)
	c.enqueueMachineSet(ng)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// node groups will be handled in parallel.
func (c *MachineSetController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting node group controller")
	defer glog.Infof("Shutting down node group controller")

	if !controller.WaitForCacheSync("machineset", stopCh, c.machineSetsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *MachineSetController) enqueue(machineSet *clusteroperator.MachineSet) {
	key, err := controller.KeyFunc(machineSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machineSet, err))
		return
	}

	c.queue.Add(key)
}

func (c *MachineSetController) enqueueRateLimited(machineSet *clusteroperator.MachineSet) {
	key, err := controller.KeyFunc(machineSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machineSet, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a node group after the provided amount of time.
func (c *MachineSetController) enqueueAfter(machineSet *clusteroperator.MachineSet, after time.Duration) {
	key, err := controller.KeyFunc(machineSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machineSet, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *MachineSetController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *MachineSetController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *MachineSetController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing node group %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping node group %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncMachineSet will sync the node group with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *MachineSetController) syncMachineSet(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing node group %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing node group %q (%v)", key, time.Since(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid machineset key %q: either namespace or name is missing", key)
	}

	machineSet, err := c.machineSetsLister.MachineSets(ns).Get(name)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("Node group %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	ng := machineSet.DeepCopy()

	glog.V(4).Infof("Provisioning node group %q", ng.Name)

	return nil
}
