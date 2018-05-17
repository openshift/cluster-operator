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

package nodeconfig

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/openshift/cluster-operator/pkg/ansible"
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	"github.com/openshift/cluster-operator/pkg/logging"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a machineset.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	// TODO: move to config map for the controllers once we have one
	// Re-execute the job every 2-4 hours.
	configManagementInterval = 2 * time.Hour

	controllerLogName = "nodeconfig"

	nodeConfigPlaybook = "playbooks/cluster-operator/node-config-daemonset.yml"
	// jobPrefix is used when generating a name for the configmap and job used for each
	// Ansible execution.
	jobType = "nodeconfig"
)

var machineSetKind = clusteroperator.SchemeGroupVersion.WithKind("MachineSet")

// NewController returns a new *Controller.
func NewController(
	clusterInformer informers.ClusterInformer,
	machineSetInformer informers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient clusteroperatorclientset.Interface,
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
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodeconfig"),
		logger:     logger,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(jobType, machineSetKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	c.jobSync = controller.NewJobSync(c.jobControl, &jobSyncStrategy{controller: c}, false, logger)

	c.syncHandler = c.jobSync.Sync
	c.enqueueMachineSet = c.enqueue
	c.ansibleGenerator = ansible.NewJobGenerator()

	return c
}

// Controller manages launching node config daemonset setup on machines
// that are masters in the cluster.
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

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// jobsSynced returns true if the job shared informer has been synced at least once.
	jobsSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	clusterLogger := logging.WithCluster(c.logger, cluster)
	clusterLogger.Debugf("Adding cluster")
	masterMachineSetName := cluster.Status.MasterMachineSetName
	if masterMachineSetName == "" {
		clusterLogger.Debug("No master machineset yet")
		return
	}
	machineSet, err := c.machineSetsLister.MachineSets(cluster.Namespace).Get(masterMachineSetName)
	if err != nil {
		clusterLogger.Warnf("Could not retrieve master machineset")
		return
	}
	c.enqueueMachineSet(machineSet)
}

func (c *Controller) updateCluster(old, cur interface{}) {
	cluster := cur.(*clusteroperator.Cluster)
	clusterLogger := logging.WithCluster(c.logger, cluster)
	clusterLogger.Debugf("Updating cluster")
	masterMachineSetName := cluster.Status.MasterMachineSetName
	if masterMachineSetName == "" {
		clusterLogger.Debug("No master machineset yet")
		return
	}
	machineSet, err := c.machineSetsLister.MachineSets(cluster.Namespace).Get(masterMachineSetName)
	if err != nil {
		clusterLogger.Warnf("Could not retrieve master machineset")
		return
	}
	c.enqueueMachineSet(machineSet)
}

func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(*clusteroperator.MachineSet)
	if !isMasterMachineSet(machineSet) {
		return
	}
	logging.WithMachineSet(c.logger, machineSet).Debugf("Adding master machine set")
	c.enqueueMachineSet(machineSet)
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	machineSet := cur.(*clusteroperator.MachineSet)
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

	c.logger.Infof("Starting nodeconfig controller")
	defer c.logger.Infof("Shutting down nodeconfig controller")

	if !controller.WaitForCacheSync("nodeconfig", stopCh, c.machineSetsSynced, c.jobsSynced) {
		c.logger.Errorf("Could not sync caches for nodeconfig controller")
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

// enqueueAfter will enqueue a machine set after the provided amount of time.
func (c *Controller) enqueueAfter(ms *clusteroperator.MachineSet, after time.Duration) {
	key, err := controller.KeyFunc(ms)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", ms, err))
		return
	}

	c.queue.AddAfter(key, after)
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

	if c.queue.NumRequeues(key) < maxRetries {
		logger.Infof("Error syncing master machine set: %v", err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("Dropping master machine set out of the queue: %v", err)
	c.queue.Forget(key)
}

func isMasterMachineSet(machineSet *clusteroperator.MachineSet) bool {
	return machineSet.Spec.NodeType == clusteroperator.NodeTypeMaster
}

type jobFactory func(string) (*v1batch.Job, *kapi.ConfigMap, error)

func (f jobFactory) BuildJob(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
	return f(name)
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

	msLog := logging.WithMachineSet(s.controller.logger, machineSet).WithField("func", "DoesOwnerNeedProcessing")
	cluster, err := controller.ClusterForMachineSet(machineSet, s.controller.clustersLister)
	if err != nil {
		s.controller.logger.Warn("could not get cluster for machine set", err)
		return false
	}
	if !cluster.Status.ControlPlaneInstalled {
		return false
	}

	// Process if the machine set generation has changed since we last ran:
	if machineSet.Status.NodeConfigInstalledJobGeneration != machineSet.Generation {
		msLog.WithFields(log.Fields{
			"NodeConfigInstalledJobGeneration": machineSet.Status.NodeConfigInstalledJobGeneration,
			"Generation":                       machineSet.Generation,
		}).Debug("machine set generation has changed")
		return true
	}
	return false
}

func (s *jobSyncStrategy) GetReprocessInterval() *time.Duration {
	tempInterval := configManagementInterval
	return &tempInterval
}

func (s *jobSyncStrategy) GetLastJobSuccess(owner metav1.Object) *time.Time {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return nil
	}
	if machineSet.Status.NodeConfigInstalledTime != nil {
		return &machineSet.Status.NodeConfigInstalledTime.Time
	}
	return nil
}

func (s *jobSyncStrategy) GetJobFactory(owner metav1.Object, deleting bool) (controller.JobFactory, error) {
	if deleting {
		return nil, fmt.Errorf("should not be undoing on deletes")
	}
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
		clusterRef := metav1.GetControllerOf(machineSet)
		if clusterRef == nil {
			return nil, nil, fmt.Errorf("machineset not owned by a cluster")
		}
		vars, err := ansible.GenerateClusterWideVarsForMachineSet(true /*isMaster*/, clusterRef.Name, &machineSet.Spec.ClusterHardware, cv)
		if err != nil {
			return nil, nil, err
		}
		image, pullPolicy := ansible.GetAnsibleImageForClusterVersion(cv)
		job, configMap := s.controller.ansibleGenerator.GeneratePlaybookJob(
			name,
			&machineSet.Spec.ClusterHardware,
			nodeConfigPlaybook,
			ansible.DefaultInventory,
			vars,
			image,
			pullPolicy,
		)
		labels := controller.JobLabelsForMachineSetController(machineSet, jobType)
		controller.AddLabels(job, labels)
		controller.AddLabels(configMap, labels)
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
	msLog := logging.WithMachineSet(s.controller.logger, machineSet)
	msLog.WithFields(log.Fields{
		"conditionType":        convertJobSyncConditionType(conditionType),
		"status":               status,
		"reason":               reason,
		"message":              message,
		"updateConditionCheck": updateConditionCheck,
	}).Debugf("SetOwnerJobSyncCondition")

	controller.SetMachineSetCondition(
		machineSet,
		convertJobSyncConditionType(conditionType),
		status,
		reason,
		message,
		updateConditionCheck,
	)
}

func (s *jobSyncStrategy) OnJobCompletion(owner metav1.Object, job *v1batch.Job, succeeded bool) {
	machineSet, ok := owner.(*clusteroperator.MachineSet)
	if !ok {
		s.controller.logger.Warn("could not convert owner from JobSync into a machineset: %#v", owner)
		return
	}
	msLog := logging.WithMachineSet(s.controller.logger, machineSet).WithFields(log.Fields{
		"func":      "OnJobCompletion",
		"job":       job.Name,
		"succeeded": succeeded,
	})
	msLog.Debug("running")
	machineSet.Status.NodeConfigInstalled = succeeded
	machineSet.Status.NodeConfigInstalledJobGeneration = machineSet.Generation

	msLog.WithFields(log.Fields{
		"job":       job.Name,
		"completed": job.Status.CompletionTime}).Infoln("found successful job")
	machineSet.Status.NodeConfigInstalledTime = job.Status.CompletionTime

	// We often sync before we hit this reprocess interval, if the threshold for
	// reprocessing happens to be exceeded on one of those syncs we may actually launch a
	// new job earlier here.
	reprocessDuration := wait.Jitter(configManagementInterval, 1.0)
	msLog.Debugf("reprocessing machine set in: %v", reprocessDuration)
	s.controller.enqueueAfter(machineSet, reprocessDuration)
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
		return clusteroperator.MachineSetNodeConfigInstalling
	case controller.JobSyncProcessed:
		return clusteroperator.MachineSetNodeConfigInstalled
	case controller.JobSyncProcessingFailed:
		return clusteroperator.MachineSetNodeConfigInstallationFailed
	default:
		return clusteroperator.MachineSetConditionType("")
	}
}
