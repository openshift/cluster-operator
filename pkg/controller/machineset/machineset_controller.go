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
	"strings"
	"time"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/cluster-operator/pkg/ansible"
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

	controllerLogName = "machineSet"

	// jobPrefix is used when generating a name for the configmap and job used for each
	// Ansible execution.
	jobPrefix = "provision-machineset-"

	masterProvisioningPlaybook = "playbooks/aws/openshift-cluster/provision.yml"

	computeProvisioningPlaybook = "playbooks/aws/openshift-cluster/provision_nodes.yml"

	deprovisioningPlaybook = "playbooks/aws/openshift-cluster/uninstall_nodes.yml"

	masterDeprovisioningPlaybook = "playbooks/aws/openshift-cluster/uninstall_masters.yml"
)

var (
	machineSetKind = clusteroperator.SchemeGroupVersion.WithKind("MachineSet")
)

// NewController returns a new *Controller.
func NewController(
	machineSetInformer informers.MachineSetInformer,
	machineInformer informers.MachineInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient clusteroperatorclientset.Interface,
	ansibleImage string,
	ansibleImagePullPolicy kapi.PullPolicy,
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
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineSet"),
		logger:     logger,
	}

	c.machineLister = machineInformer.Lister()

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		DeleteFunc: c.deleteMachineSet,
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(jobPrefix, machineSetKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	c.jobSync = controller.NewJobSync(c.jobControl, &jobSyncStrategy{controller: c}, true, logger)

	c.syncHandler = c.jobSync.Sync
	c.syncHandler2 = c.syncMachineSet
	c.enqueueMachineSet = c.enqueue
	c.ansibleGenerator = ansible.NewJobGenerator(ansibleImage, ansibleImagePullPolicy)

	return c
}

// Controller manages provisioning machine sets.
type Controller struct {
	client     clusteroperatorclientset.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncMachineSet for testing.
	syncHandler func(hKey string) error

	// Handle machineset->machine object lifecycle
	syncHandler2 func(hKey string) error

	// To allow injection of mock ansible generator for testing
	ansibleGenerator ansible.JobGenerator

	jobControl controller.JobControl

	jobSync controller.JobSync

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

	// jobsSynced returns true if the job shared informer has been synced at least once.
	jobsSynced cache.InformerSynced

	// MachineSets that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addMachineSet(obj interface{}) {
	ms := obj.(*clusteroperator.MachineSet)
	colog.WithMachineSet(c.logger, ms).Debugf("enqueueing added machine set")
	c.enqueueMachineSet(ms)
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	ms := cur.(*clusteroperator.MachineSet)
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
	colog.WithMachineSet(c.logger, ms).Debugf("enqueueing deleted machine set")
	c.enqueueMachineSet(ms)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machine sets will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("Starting machine set controller")
	defer c.logger.Infof("Shutting down machine set controller")

	if !controller.WaitForCacheSync("machineset", stopCh, c.machineSetsSynced, c.jobsSynced) {
		c.logger.Errorf("Could not sync caches for machineset controller")
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

	// create/delete ansible jobs
	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	// create/delete Machine objects
	err = c.syncHandler2(key.(string))
	if err != nil {
		c.logger.Errorf("JDIAZ error handing machine objects")
	}

	return true
}

func (c *Controller) syncMachineSet(key string) error {
	c.logger.WithField("key", key).Debugf("JDIAZ synching machineset %v", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Errorf("JDIAZ error parsing namespace out of machineset name %v", key)
		return err
	}

	ms, err := c.machineSetsLister.MachineSets(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Errorf("JDIAZ couldn't find machineset %v", key)
		return err
	}
	if err != nil {
		c.logger.Errorf("JDIAZ problem fetching machineset %v", key)
		return err
	}
	if ms.Spec.NodeType == "Master" {
		// masters aren't cattle, they need ansible depoyment
		c.logger.WithField("machineset", ms).Debugf("JDIAZ ignoring master machineset: %v", ms)
		return nil
	}
	//c.logger.WithField("machineset", ms).Debugf("JDIAZ full machineset: %+v", ms)

	// check DeletionTimestamp???

	// get all the machines in our current namespace
	allMachines, err := c.machineLister.Machines(namespace).List(labels.Everything())
	if err != nil {
		c.logger.WithField("machineset", ms).Errorf("JDIAZ error while trying to get list of all machines")
		return err
	}
	c.logger.WithField("machineset", name).Debugf("JDIAZ machinelist: %v", allMachines)

	// get all machines which are children of this machineset
	var filteredMachines []*clusteroperator.Machine
	for _, machine := range allMachines {
		if machine.DeletionTimestamp != nil {
			continue
		}
		controllerRef := metav1.GetControllerOf(machine)
		if controllerRef == nil {
			c.logger.WithField("machineset", name).Debugf("JDIAZ no controller ref for machine %v", machine)
			continue
		}
		if ms.UID != controllerRef.UID {
			//c.logger.WithField("machineset", name).Debug("JDIAZ the kid is not my son %v", machine)
			continue
		}
		filteredMachines = append(filteredMachines, machine)
	}
	//c.logger.WithField("machineset", name).Debugf("JDIAZ children machines: %v", filteredMachines)

	var manageMachinesErr error

	//if machineSetNeedsSync && ms.DeletionTimestamp == nil {
	if true && ms.DeletionTimestamp == nil {
		manageMachinesErr = c.manageMachines(filteredMachines, ms)
	}

	if manageMachinesErr != nil {
		return manageMachinesErr
	}

	return nil
}

func (c *Controller) manageMachines(machines []*clusteroperator.Machine, ms *clusteroperator.MachineSet) error {
	/*machineSetKey, err := controller.KeyFunc(ms)
	if err != nil {
		// FIXME
		return nil
	}*/

	machinePrefix := ms.ObjectMeta.Name + "-"
	//c.logger.WithField("machineset", ms.ObjectMeta.Name).Debugf("JDIAZ machine prefix: %v", machinePrefix)

	machineSetMachines := []*clusteroperator.Machine{}
	//machinesToCreate := []*clusteroperator.Machine{}
	machinesToDelete := []*clusteroperator.Machine{}
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
		if ! strings.HasPrefix(machine.ObjectMeta.Name, machinePrefix) {
			// poorly-named
			c.logger.WithField("machineset", ms.ObjectMeta.Name).Infof("JDIAZ child machine without expected prefix %v", machine.ObjectMeta.Name)
			machinesToDelete = append(machinesToDelete, machine)
			createCount++
			deleteCount++
			continue
		}

		if currentMachineNeedsUpdate(machine, ms) {
			c.logger.WithField("machineset", ms.ObjectMeta.Name).Errorf("JDIAZ child machine needs update %v", machine.ObjectMeta.Name)
			machinesToDelete = append(machinesToDelete, machine)
			createCount++
			deleteCount++
			continue
		}

		machineSetMachines = append(machineSetMachines, machine)

	}

	// Sync machines
	machineConfig := buildNewMachine(ms, machinePrefix)
	c.logger.WithField("machineset", ms.ObjectMeta.Name).Debugf("JDIAZ machine template: %+v", machineConfig)

	for i := 0 ; i < createCount ; i++ {
		_, err := c.client.ClusteroperatorV1alpha1().Machines(ms.ObjectMeta.Namespace).Create(machineConfig)
		if err != nil {
			// FIXME add to error list?
			c.logger.WithField("machineset", ms.ObjectMeta.Name).Error("JDIAZ error creating machine...")
			// update expectations?
		}
	}

	c.logger.WithField("machineset", ms.ObjectMeta.Name).Debug("JDIAZ delete count :v", deleteCount)
	for _, m := range machinesToDelete {
		c.logger.WithField("machineset", ms.ObjectMeta.Name).Debugf("JDIAZ deleting machine %v", m.Name)
		c.client.ClusteroperatorV1alpha1().Machines(ms.ObjectMeta.Namespace).Delete(m.Name, &metav1.DeleteOptions{})
		// catch error ^^^
		deleteCount--
	}

	var machineToDelete *clusteroperator.Machine
	c.logger.WithField("machineset", ms.ObjectMeta.Name).Debugf("JDIAZ deleting %v more machines", deleteCount)
	for i := 0 ; i < deleteCount ; i++ {
		machineToDelete, machineSetMachines = machineSetMachines[0], machineSetMachines[1:]
		c.client.ClusteroperatorV1alpha1().Machines(ms.ObjectMeta.Namespace).Delete(machineToDelete.Name, &metav1.DeleteOptions{})
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
			NodeType: "Compute",
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

type jobOwnerControl struct {
	controller *Controller
}

func (c *jobOwnerControl) GetOwnerKey(owner metav1.Object) (string, error) {
	return controller.KeyFunc(owner)
}

func (c *jobOwnerControl) GetOwner(namespace string, name string) (metav1.Object, error) {
	return c.controller.machineSetsLister.MachineSets(namespace).Get(name)
}

func (c *jobOwnerControl) OnOwnedJobEvent(owner metav1.Object) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		c.controller.logger.WithFields(log.Fields{"owner": owner.GetName(), "namespace": owner.GetNamespace()}).
			Errorf("attempt to enqueue owner that is not a machineset")
		return
	}
	c.controller.enqueueMachineSet(machineSet)
}

type jobFactory func(string) (*v1batch.Job, *kapi.ConfigMap, error)

func (f jobFactory) BuildJob(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
	return f(name)
}

type jobSyncStrategy struct {
	controller *Controller
}

func (s *jobSyncStrategy) GetOwner(key string) (metav1.Object, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}
	if len(namespace) == 0 || len(name) == 0 {
		return nil, fmt.Errorf("invalid key %q: either namespace or name is missing", key)
	}
	return s.controller.machineSetsLister.MachineSets(namespace).Get(name)
}

func (s *jobSyncStrategy) DoesOwnerNeedProcessing(owner metav1.Object) bool {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return false
	}
	return machineSet.Status.ProvisionedJobGeneration != machineSet.Generation
}

func (s *jobSyncStrategy) GetJobFactory(owner metav1.Object, deleting bool) (controller.JobFactory, error) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		return nil, fmt.Errorf("could not convert owner from JobSync into a machineset")
	}
	return jobFactory(func(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
		cvRef := machineSet.Spec.ClusterVersionRef
		cv, err := s.controller.client.Clusteroperator().ClusterVersions(cvRef.Namespace).Get(cvRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}

		vars, err := ansible.GenerateMachineSetVars(machineSet, cv)
		if err != nil {
			return nil, nil, err
		}
		var playbook string
		switch {
		case deleting:
			switch machineSet.Spec.NodeType {
			case clusteroperator.NodeTypeMaster:
				playbook = masterDeprovisioningPlaybook
			default:
				playbook = deprovisioningPlaybook
			}
		case machineSet.Spec.NodeType == clusteroperator.NodeTypeMaster:
			playbook = masterProvisioningPlaybook
		default:
			playbook = computeProvisioningPlaybook
		}
		job, configMap := s.controller.ansibleGenerator.GeneratePlaybookJob(
			name,
			&machineSet.Spec.ClusterHardware,
			playbook,
			ansible.DefaultInventory,
			vars,
		)
		return job, configMap, nil
	}), nil
}

func (s *jobSyncStrategy) DeepCopyOwner(owner metav1.Object) metav1.Object {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return machineSet
	}
	return machineSet.DeepCopy()
}

func (s *jobSyncStrategy) SetOwnerJobSyncCondition(
	owner metav1.Object,
	conditionType controller.JobSyncConditionType,
	status kapi.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck controller.UpdateConditionCheck,
) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return
	}
	controller.SetMachineSetCondition(
		machineSet,
		convertJobSyncConditionType(conditionType),
		status,
		reason,
		message,
		updateConditionCheck,
	)
}

func (s *jobSyncStrategy) OnJobCompletion(owner metav1.Object) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return
	}
	machineSet.Status.Provisioned = true
	machineSet.Status.ProvisionedJobGeneration = machineSet.Generation
}

func (s *jobSyncStrategy) UpdateOwnerStatus(original, owner metav1.Object) error {
	originalMachineSet, ok := original.(*clusteroperator.MachineSet)
	if !ok {
		return fmt.Errorf("could not convert original from JobSync into a machineset")
	}
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		return fmt.Errorf("could not convert owner from JobSync into a machineset")
	}
	return controller.PatchMachineSetStatus(s.controller.client, originalMachineSet, machineSet)
}

func convertJobSyncConditionType(conditionType controller.JobSyncConditionType) clusteroperator.MachineSetConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clusteroperator.MachineSetHardwareProvisioning
	case controller.JobSyncProcessed:
		return clusteroperator.MachineSetHardwareProvisioned
	case controller.JobSyncProcessingFailed:
		return clusteroperator.MachineSetHardwareProvisioningFailed
	case controller.JobSyncUndoing:
		return clusteroperator.MachineSetHardwareDeprovisioning
	case controller.JobSyncUndoFailed:
		return clusteroperator.MachineSetHardwareDeprovisioningFailed
	default:
		return clusteroperator.MachineSetConditionType("")
	}
}
