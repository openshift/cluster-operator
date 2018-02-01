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

package machine

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

// NewMachineController returns a new *MachineController.
func NewMachineController(machineInformer informers.MachineInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *MachineController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_machine_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &MachineController{
		client: clusteroperatorClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machine"),
	}

	machineInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachine,
		UpdateFunc: c.updateMachine,
		DeleteFunc: c.deleteMachine,
	})
	c.machinesLister = machineInformer.Lister()
	c.machinesSynced = machineInformer.Informer().HasSynced

	c.syncHandler = c.syncMachine
	c.enqueueMachine = c.enqueue

	return c
}

// MachineController monitors machines creating in MachineGroups.
type MachineController struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncMachine for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueMachine func(machine *clusteroperator.Machine)

	// machinesLister is able to list/get machines and is populated by the shared informer passed to
	// NewMachineController.
	machinesLister lister.MachineLister
	// machinesSynced returns true if the machine shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machinesSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *MachineController) addMachine(obj interface{}) {
	h := obj.(*clusteroperator.Machine)
	glog.V(4).Infof("Adding machine %s", h.Name)
	c.enqueueMachine(h)
}

func (c *MachineController) updateMachine(old, cur interface{}) {
	oldH := old.(*clusteroperator.Machine)
	curH := cur.(*clusteroperator.Machine)
	glog.V(4).Infof("Updating machine %s", oldH.Name)
	c.enqueueMachine(curH)
}

func (c *MachineController) deleteMachine(obj interface{}) {
	h, ok := obj.(*clusteroperator.Machine)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		h, ok = tombstone.Obj.(*clusteroperator.Machine)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Machine %#v", obj))
			return
		}
	}
	glog.V(4).Infof("Deleting machine %s", h.Name)
	c.enqueueMachine(h)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// machines will be handled in parallel.
func (c *MachineController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting machine controller")
	defer glog.Infof("Shutting down machine controller")

	if !controller.WaitForCacheSync("machine", stopCh, c.machinesSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *MachineController) enqueue(machine *clusteroperator.Machine) {
	key, err := controller.KeyFunc(machine)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machine, err))
		return
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *MachineController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *MachineController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *MachineController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing machine %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping machine %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncMachine will sync the machine with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *MachineController) syncMachine(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing machine %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing machine %q (%v)", key, time.Since(startTime))
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
		glog.V(2).Infof("Machine %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	h := machine.DeepCopy()

	glog.V(4).Infof("Reconciling machine %q", h.Name)

	return nil
}
