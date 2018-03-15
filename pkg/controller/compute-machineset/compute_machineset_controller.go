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

package computemachineset

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	colog "github.com/openshift/cluster-operator/pkg/logging"
)

var controllerKind = clusteroperator.SchemeGroupVersion.WithKind("MachineSet")

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	controllerLogName = "computeMachineSet"

)

var (
	machineSetKind = clusteroperator.SchemeGroupVersion.WithKind("MachineSet")
)

// NewController returns a new *Controller.
func NewController(
	machineSetInformer informers.MachineSetInformer,
	machineInformer informers.MachineInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient clusteroperatorclientset.Interface,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_machine_set_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)

	c := &Controller{
		client:     clusteroperatorClient,
		expectations: controller.NewUIDTrackingExpectations(controller.NewExpectations()),
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "computeMachineSet"),
		logger:     logger,
	}

	c.machineLister = machineInformer.Lister()

	machineInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachine,
		UpdateFunc: c.updateMachine,
		DeleteFunc: c.deleteMachine,
	})

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

// Controller manages provisioning machine sets.
type Controller struct {
	client     clusteroperatorclientset.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncMachineSet for testing.
	syncHandler func(hKey string) error

	// cache of creates/deletes the machineset expects to see
	expectations *controller.UIDTrackingExpectations

	// used for unit testing
	enqueueMachineSet func(machineSet *clusteroperator.MachineSet)

	// fetch machine objects
	machineLister lister.MachineLister

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// MachineSets that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addMachine(obj interface{}) {
	machine := obj.(*clusteroperator.Machine)
	glog.V(6).Infof("new machine added %v", machine.Name)

	machineSet, err := controller.MachineSetForMachine(machine, c.machineSetsLister)
	if err != nil {
		glog.V(2).Infof("error retrieving machineSet for machine %q/%q: %v", machine.Namespace, machine.Name, err)
		return
	}
	if machineSet == nil {
		glog.V(6).Infof("machine %q/%q added that is not controlled by a machineSet", machine.Namespace, machine.Name)
		return
	}

	machineSetKey, err := controller.KeyFunc(machineSet)
	if err != nil {
		return
	}

	c.expectations.CreationObserved(machineSetKey)
	c.enqueueMachineSet(machineSet)
}



func (c *Controller) updateMachine(old, cur interface{}) {
	machine := cur.(*clusteroperator.Machine)
	glog.V(2).Infof("machine updated??? %v", machine.Name)
}

func (c *Controller) deleteMachine(obj interface{}) {
	machine, ok := obj.(*clusteroperator.Machine)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		machine, ok = tombstone.Obj.(*clusteroperator.Machine)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Machine %#v", obj))
			return
		}
	}
	glog.V(6).Infof("machine deleted %v", machine.Name)

	machineSet, err := controller.MachineSetForMachine(machine, c.machineSetsLister)
	if err != nil {
		glog.V(2).Infof("error retrieving machineset for machine %q/%q: %v", machine.Namespace, machine.Name, err)
		return
	}
	if machineSet == nil {
		glog.V(6).Infof("machine %q/%q deleted that is not controlled by machineset", machine.Namespace, machine.Name)
		return
	}

	machineSetKey, err := controller.KeyFunc(machineSet)
	if err != nil {
		return
	}

	c.expectations.DeletionObserved(machineSetKey, getMachineKey(machine))
	c.enqueueMachineSet(machineSet)
}

func (c *Controller) addMachineSet(obj interface{}) {
	ms := obj.(*clusteroperator.MachineSet)
	if ms.Spec.NodeType == "Master" {
		return
	}

	colog.WithMachineSet(c.logger, ms).Debugf("enqueueing added machine set")
	c.enqueueMachineSet(ms)
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	ms := cur.(*clusteroperator.MachineSet)
	if ms.Spec.NodeType == "Master" {
		return
	}

	colog.WithMachineSet(c.logger, ms).Debugf("enqueueing updated machine set")
	c.enqueueMachineSet(ms)
}

func (c *Controller) deleteMachineSet(obj interface{}) {
	ms, ok := obj.(*clusteroperator.MachineSet)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		ms, ok = tombstone.Obj.(*clusteroperator.MachineSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a MachineSet %#v", obj))
			return
		}
	}
	if ms.Spec.NodeType == "Master" {
		return
	}

	colog.WithMachineSet(c.logger, ms).Debugf("enqueueing deleted machine set")
	c.enqueueMachineSet(ms)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machine sets will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("Starting compute machine set controller")
	defer c.logger.Infof("Shutting down compute machine set controller")

	if !controller.WaitForCacheSync("computemachineset", stopCh, c.machineSetsSynced) {
		c.logger.Errorf("Could not sync caches for computemachineset controller")
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(machineSet *clusteroperator.MachineSet) {
	key, err := controller.KeyFunc(machineSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", machineSet, err))
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

	// create/delete Machine objects
	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *Controller) ensureMachinesAreDeleted(ms *clusteroperator.MachineSet) error {
	machines, err := controller.MachinesForMachineSet(ms, c.machineLister)
	if err != nil {
		return err
	}
	errs := []error{}
	for _, machine := range machines {
		if machine.DeletionTimestamp == nil {
			err = c.client.ClusteroperatorV1alpha1().Machines(machine.Namespace).Delete(machine.Name, &metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}
	return nil
}
func (c *Controller) syncMachineSet(key string) error {
	c.logger.WithField("key", key).Debug("synching machineset")

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Errorf("error parsing namespace out of machineset name %v", key)
		return err
	}

	ms, err := c.machineSetsLister.MachineSets(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Errorf("couldn't find machineset %v", key)
		c.expectations.DeleteExpectations(key)
		return err
	}
	if err != nil {
		c.logger.Errorf("problem fetching machineset %v: %v", key, err)
		return err
	}

	msLog := colog.WithMachineSet(c.logger, ms)

	if ms.DeletionTimestamp != nil {
		msLog.Debug("machineset has been deleted. Ensuring that its machines are also deleted.")
		return c.ensureMachinesAreDeleted(ms)
	}


	// get all the machines in our current namespace
	allMachines, err := c.machineLister.Machines(namespace).List(labels.Everything())
	if err != nil {
		c.logger.WithField("machineset", ms).Errorf("error while trying to get list of all machines")
		return err
	}

	// get all machines which are children of this machineset
	var filteredMachines []*clusteroperator.Machine
	for _, machine := range allMachines {
		if machine.DeletionTimestamp != nil {
			continue
		}
		controllerRef := metav1.GetControllerOf(machine)
		if controllerRef == nil {
			c.logger.WithField("machineset", name).Debugf("no controller ref for machine %v", machine)
			continue
		}
		if ms.UID != controllerRef.UID {
			//c.logger.WithField("machineset", name).Debug("the kid is not my son %v", machine)
			continue
		}
		filteredMachines = append(filteredMachines, machine)
	}

	var manageMachinesErr error

	machineSetNeedsSync := c.expectations.SatisfiedExpectations(key)

	if machineSetNeedsSync && ms.DeletionTimestamp == nil {
		manageMachinesErr = c.manageMachines(filteredMachines, ms)
	}

	// update Status field
	/***
	original := ms
	newms = ms.DeepCopy()
	***/

	if manageMachinesErr != nil {
		return manageMachinesErr
	}


	return nil
}

func (c *Controller) manageMachines(machines []*clusteroperator.Machine, ms *clusteroperator.MachineSet) error {
	machineSetKey, err := controller.KeyFunc(ms)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for MachineSet %#v: %v", ms, err))
		return nil
	}

	machinePrefix := ms.Name + "-"
	//c.logger.WithField("machineset", ms.ObjectMeta.Name).Debugf("JDIAZ machine prefix: %v", machinePrefix)

	machineSetMachines := []*clusteroperator.Machine{}
	//machinesToCreate := []*clusteroperator.Machine{}
	machinesToDelete := []string{}
	//machinesToUpdate := []*clusteroperator.Machine{}

	deleteCount := int(0)
	createCount := int(0)


	// caculate how many to create/delete
	if len(machines) > ms.Spec.Size {
		deleteCount = len(machines) - ms.Spec.Size
	}
	if len(machines) < ms.Spec.Size {
		createCount = ms.Spec.Size - len(machines)
	}

	for _, machine := range machines {
		/*
		if currentMachineNeedsUpdate(machine, ms) {
			c.logger.WithField("machineset", ms.Name).Errorf("JDIAZ child machine needs update %v", machine.Name)
			machinesToDelete = append(machinesToDelete, machine)
			createCount++
			deleteCount++
			continue
		}
		*/

		machineSetMachines = append(machineSetMachines, machine)

	}

	for i := 0 ; i < deleteCount; i++ {
		machinesToDelete = append(machinesToDelete, getMachineKey(machineSetMachines[i]))
	}
	c.logger.WithField("machineset", ms.Name).Infof("JDIAZ creating %v, deleting %+v", createCount, machinesToDelete)
	c.expectations.SetExpectations(machineSetKey, createCount, machinesToDelete)

	// Sync machines
	machineConfig := buildNewMachine(ms, machinePrefix)
	c.logger.WithField("machineset", ms.Name).Debugf("JDIAZ machine template: %+v", machineConfig)

	for i := 0 ; i < createCount ; i++ {
		_, err := c.client.ClusteroperatorV1alpha1().Machines(ms.ObjectMeta.Namespace).Create(machineConfig)
		if err != nil {
			// FIXME add to error list?
			c.logger.WithField("machineset", ms.Name).Error("JDIAZ error creating machine...")
			// update expectations?
		}
	}

	c.logger.WithField("machineset", ms.Name).Debugf("JDIAZ delete count :%v", deleteCount)
	/*
	for _, m := range machinesToDelete {
		c.logger.WithField("machineset", ms.Name).Debugf("JDIAZ deleting machine %v", m.Name)
		c.client.ClusteroperatorV1alpha1().Machines(ms.ObjectMeta.Namespace).Delete(m.Name, &metav1.DeleteOptions{})
		// catch error ^^^
		deleteCount--
	}
	*/

	c.logger.WithField("machineset", ms.Name).Debugf("JDIAZ deleting %v more machines", deleteCount)
	for i := 0 ; i < deleteCount ; i++ {
		c.client.ClusteroperatorV1alpha1().Machines(ms.ObjectMeta.Namespace).Delete(machineSetMachines[i].Name, &metav1.DeleteOptions{})
		// catch error ^^^
	}

	return nil
}

func currentMachineNeedsUpdate(machine *clusteroperator.Machine, ms *clusteroperator.MachineSet) bool {
	// compare machine settings with machineset settings to see if we have an out-of-date machine
	if machine.Spec.NodeType != ms.Spec.NodeType {
		return true
	}
	return false
}
func buildNewMachine(machineSet *clusteroperator.MachineSet, machineNamePrefix string) *clusteroperator.Machine {
	machineConfig := &clusteroperator.Machine{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:	machineNamePrefix,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(machineSet, controllerKind)},
		},
		Spec: clusteroperator.MachineSpec{
			NodeType: machineSet.Spec.NodeType,
		},
	}

	return machineConfig
}
func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	logger := c.logger.WithField("machineset", key)

	logger.Errorf("error syncing machine set: %v", err)
	if c.queue.NumRequeues(key) < maxRetries {
		logger.Infof("retrying machine set")
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("dropping machine set out of the queue: %v", err)
	c.queue.Forget(key)
}

func getMachineKey(machine *clusteroperator.Machine) string {
	return fmt.Sprintf("%s/%s", machine.Namespace, machine.Name)
}
