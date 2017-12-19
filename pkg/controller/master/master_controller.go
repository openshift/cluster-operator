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

package master

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

// NewMasterController returns a new *MasterController.
func NewMasterController(machineInformer informers.MachineInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *MasterController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_master_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &MasterController{
		client: clusteroperatorClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "master"),
	}

	machineInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachine,
		UpdateFunc: c.updateMachine,
		DeleteFunc: c.deleteMachine,
	})
	c.machinesLister = machineInformer.Lister()
	c.machinesSynced = machineInformer.Informer().HasSynced

	c.syncHandler = c.syncMasterMachine
	c.enqueueMasterMachine = c.enqueue

	return c
}

// MasterController manages launching the master/control plane on machines
// that are masters in the cluster.
type MasterController struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncMachine for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueMasterMachine func(masterMachine *clusteroperator.Machine)

	// machinesLister is able to list/get machines and is populated by the shared informer passed to
	// NewMasterController.
	machinesLister lister.MachineLister
	// machinesSynced returns true if the machine shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machinesSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *MasterController) addMachine(obj interface{}) {
	n := obj.(*clusteroperator.Machine)
	if !isMasterMachine(n) {
		return
	}
	glog.V(4).Infof("Adding master machine %s", n.Name)
	c.enqueueMasterMachine(n)
}

func (c *MasterController) updateMachine(old, cur interface{}) {
	oldN := old.(*clusteroperator.Machine)
	curN := cur.(*clusteroperator.Machine)
	if !isMasterMachine(curN) {
		return
	}
	glog.V(4).Infof("Updating master machine %s", oldN.Name)
	c.enqueueMasterMachine(curN)
}

func (c *MasterController) deleteMachine(obj interface{}) {
	n, ok := obj.(*clusteroperator.Machine)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		n, ok = tombstone.Obj.(*clusteroperator.Machine)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Machine %#v", obj))
			return
		}
	}
	if !isMasterMachine(n) {
		return
	}
	glog.V(4).Infof("Deleting master machine %s", n.Name)
	c.enqueueMasterMachine(n)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// machines will be handled in parallel.
func (c *MasterController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting master controller")
	defer glog.Infof("Shutting down master controller")

	if !controller.WaitForCacheSync("machine", stopCh, c.machinesSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *MasterController) enqueue(machine *clusteroperator.Machine) {
	key, err := controller.KeyFunc(machine)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machine, err))
		return
	}

	c.queue.Add(key)
}

func (c *MasterController) enqueueRateLimited(machine *clusteroperator.Machine) {
	key, err := controller.KeyFunc(machine)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machine, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a machine after the provided amount of time.
func (c *MasterController) enqueueAfter(machine *clusteroperator.Machine, after time.Duration) {
	key, err := controller.KeyFunc(machine)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machine, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *MasterController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *MasterController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *MasterController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing master machine %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping master machine %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncMasterMachine will sync the master machine with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *MasterController) syncMasterMachine(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing master machine %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing master machine %q (%v)", key, time.Since(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid machine key %q: either namespace or name is missing", key)
	}

	machine, err := c.machinesLister.Machines(ns).Get(name)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("Master machine %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	if !isMasterMachine(machine) {
		return nil
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	n := machine.DeepCopy()

	glog.V(4).Infof("Reconciling master machine %q", n.Name)

	return nil
}

func isMasterMachine(machine *clusteroperator.Machine) bool {
	return machine.Spec.NodeType == clusteroperator.NodeTypeMaster
}
