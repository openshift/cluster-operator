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

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

var (
	machineSetKind = clusteroperator.SchemeGroupVersion.WithKind("MachineSet")
	clusterKind    = clusteroperator.SchemeGroupVersion.WithKind("Cluster")
)

// NewController returns a new *Controller.
func NewController(
	clusterInformer informers.ClusterInformer,
	machineSetInformer informers.MachineSetInformer,
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

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		DeleteFunc: c.deleteMachineSet,
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(jobPrefix, machineSetKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	c.jobSync = controller.NewJobSync(c.jobControl, &jobSyncStrategy{controller: c}, logger)

	c.syncHandler = c.jobSync.Sync
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

	// To allow injection of mock ansible generator for testing
	ansibleGenerator ansible.JobGenerator

	jobControl controller.JobControl

	jobSync controller.JobSync

	// used for unit testing
	enqueueMachineSet func(machineSet *clusteroperator.MachineSet)

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewMachineSetController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// jobsSynced returns true of the job shared informer has been synced at least once.
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
	if ms.Spec.NodeType == clusteroperator.NodeTypeMaster && ms.Status.Installed {
		cluster, err := controller.ClusterForMachineSet(ms, c.clustersLister)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("cannot retrieve cluster for machineset %s/%s: %v", ms.Namespace, ms.Name, err))
			return
		}
		machineSets, err := c.machineSetsForCluster(cluster)
		if err != nil {
			c.logger.Errorf("cannot retrieve machine sets for cluster: %v", err)
			utilruntime.HandleError(err)
			return
		}
		for _, machineSet := range machineSets {
			if machineSet.Name != ms.Name {
				loggerForMachineSet(c.logger, machineSet).Debugf("enqueueing machine set for installed master")
				c.enqueueMachineSet(machineSet)
			}
		}
	}
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

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	logger := colog.WithCluster(c.logger, cluster)
	machineSets, err := c.machineSetsForCluster(cluster)
	if err != nil {
		logger.Errorf("Cannot retrieve machine sets for cluster: %v", err)
		utilruntime.HandleError(err)
		return
	}

	for _, machineSet := range machineSets {
		colog.WithMachineSet(logger, machineSet).Debugf("enqueueing machine set for created cluster")
		c.enqueueMachineSet(machineSet)
	}
}

func (c *Controller) updateCluster(old, obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	logger := colog.WithCluster(c.logger, cluster)
	machineSets, err := c.machineSetsForCluster(cluster)
	if err != nil {
		logger.Errorf("Cannot retrieve machine sets for cluster: %v", err)
		utilruntime.HandleError(err)
		return
	}
	for _, machineSet := range machineSets {
		colog.WithMachineSet(logger, machineSet).Debugf("enqueueing machine set for update cluster")
		c.enqueueMachineSet(machineSet)
	}
}

func (c *Controller) machineSetsForCluster(cluster *clusteroperator.Cluster) ([]*clusteroperator.MachineSet, error) {
	clusterMachineSets := []*clusteroperator.MachineSet{}
	allMachineSets, err := c.machineSetsLister.MachineSets(cluster.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, machineSet := range allMachineSets {
		if metav1.IsControlledBy(machineSet, cluster) {
			clusterMachineSets = append(clusterMachineSets, machineSet)
		}
	}
	return clusterMachineSets, nil
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machine sets will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("Starting machine set controller")
	defer c.logger.Infof("Shutting down machine set controller")

	if !controller.WaitForCacheSync("machineset", stopCh, c.machineSetsSynced, c.clustersSynced, c.jobsSynced) {
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

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
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
	if machineSet.Status.ProvisionedJobGeneration == machineSet.Generation {
		return false
	}
	cluster, err := controller.ClusterForMachineSet(machineSet, s.controller.clustersLister)
	if err != nil {
		colog.WithMachineSet(s.controller.logger, machineSet).
			Warn("could not get cluster for machine set")
		return false
	}
	if !cluster.Status.Provisioned || cluster.Status.ProvisionedJobGeneration != cluster.Generation {
		return false
	}
	switch machineSet.Spec.NodeType {
	case clusteroperator.NodeTypeMaster:
		return true
	case clusteroperator.NodeTypeCompute:
		masterMachineSet, err := s.controller.machineSetsLister.MachineSets(machineSet.Namespace).Get(cluster.Status.MasterMachineSetName)
		if err != nil {
			colog.WithCluster(colog.WithMachineSet(s.controller.logger, machineSet), cluster).
				WithField("master", cluster.Status.MasterMachineSetName).
				Warn("could not get master machine set")
			return false
		}
		// Only provision compute nodes if openshift has been installed on
		// master nodes.
		// We need to verify that the generation of the master machine set
		// has not been changed since the installation. If the generation
		// has changed, then the installation is no longer valid. The master
		// controller needs to re-work the installation first.
		masterInstalled := masterMachineSet.Status.Installed &&
			masterMachineSet.Status.InstalledJobGeneration == masterMachineSet.Generation
		return masterInstalled
	default:
		colog.WithMachineSet(s.controller.logger, machineSet).
			Warnf("unknown node type %q", machineSet.Spec.NodeType)
		return false
	}
}

func (s *jobSyncStrategy) GetJobFactory(owner metav1.Object) (controller.JobFactory, error) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		return nil, fmt.Errorf("could not convert owner from JobSync into a machineset")
	}
	cluster, err := controller.ClusterForMachineSet(machineSet, s.controller.clustersLister)
	if err != nil {
		return nil, err
	}
	return jobFactory(func(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
		vars, err := ansible.GenerateMachineSetVars(cluster, machineSet)
		if err != nil {
			return nil, nil, err
		}
		var playbook string
		if machineSet.Spec.NodeType == clusteroperator.NodeTypeMaster {
			playbook = masterProvisioningPlaybook
		} else {
			playbook = computeProvisioningPlaybook
		}
		job, configMap := s.controller.ansibleGenerator.GeneratePlaybookJob(
			name,
			&cluster.Spec.Hardware,
			playbook,
			ansible.DefaultInventory,
			vars,
		)
		return job, configMap, nil
	}), nil
}

func (s *jobSyncStrategy) GetOwnerCurrentJob(owner metav1.Object) string {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return ""
	}
	if machineSet.Status.ProvisionJob == nil {
		return ""
	}
	return machineSet.Status.ProvisionJob.Name
}

func (s *jobSyncStrategy) SetOwnerCurrentJob(owner metav1.Object, jobName string) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return
	}
	if jobName == "" {
		machineSet.Status.ProvisionJob = nil
	} else {
		machineSet.Status.ProvisionJob = &kapi.LocalObjectReference{Name: jobName}
	}
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

func (s *jobSyncStrategy) OnJobFailure(owner metav1.Object) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return
	}
	// ProvisionedJobGeneration is set even when the job failed because we
	// do not want to run the provision job again until there have been
	// changes in the spec of the machine set.
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

func (s *jobSyncStrategy) ProcessDeletedOwner(owner metav1.Object) error {
	return nil
}

func convertJobSyncConditionType(conditionType controller.JobSyncConditionType) clusteroperator.MachineSetConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clusteroperator.MachineSetHardwareProvisioning
	case controller.JobSyncProcessed:
		return clusteroperator.MachineSetHardwareProvisioned
	case controller.JobSyncProcessingFailed:
		return clusteroperator.MachineSetHardwareProvisioningFailed
	default:
		return clusteroperator.MachineSetConditionType("")
	}
}
