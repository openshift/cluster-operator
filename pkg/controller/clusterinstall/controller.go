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

package clusterinstall

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
	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	"github.com/openshift/cluster-operator/pkg/logging"
	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	capilister "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

var (
	clusterKind = capi.SchemeGroupVersion.WithKind("Cluster")
)

// NewController returns a new *Controller for cluster-api resources.
func NewController(
	controllerName string,
	installStrategy InstallStrategy,
	playbooks []string,
	clusterInformer capiinformers.ClusterInformer,
	machineSetInformer capiinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clustopclientset.Interface,
	capiClient capiclientset.Interface,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage(
			fmt.Sprintf("clusteroperator_%s_controller", controllerName),
			kubeClient.CoreV1().RESTClient().GetRateLimiter(),
		)
	}

	logger := log.WithField("controller", controllerName)

	c := &Controller{
		controllerName:    controllerName,
		installStrategy:   installStrategy,
		playbooks:         playbooks,
		clustopClient:     clustopClient,
		capiClient:        capiClient,
		kubeClient:        kubeClient,
		queue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName),
		Logger:            logger,
		clusterLister:     clusterInformer.Lister(),
		machineSetLister:  machineSetInformer.Lister(),
		clustersSynced:    clusterInformer.Informer().HasSynced,
		machineSetsSynced: machineSetInformer.Informer().HasSynced,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		DeleteFunc: c.deleteMachineSet,
	})

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(controllerName, clusterKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	syncStrategy := (controller.JobSyncStrategy)(&jobSyncStrategy{controller: c})
	if _, shouldReprocessJobs := installStrategy.(controller.JobSyncReprocessStrategy); shouldReprocessJobs {
		syncStrategy = &jobSyncWithReprocessStrategy{&jobSyncStrategy{controller: c}}
	}
	c.jobSync = controller.NewJobSync(c.jobControl, syncStrategy, false, logger)

	c.syncHandler = c.jobSync.Sync
	c.enqueueCluster = c.enqueue
	c.ansibleGenerator = ansible.NewJobGenerator()

	return c
}

// Controller manages installing on a cluster software that runs on the master
// machine set.
type Controller struct {
	controllerName  string
	installStrategy InstallStrategy
	playbooks       []string

	clustopClient clustopclientset.Interface
	capiClient    capiclientset.Interface
	kubeClient    kubeclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error

	// To allow injection of mock ansible generator for testing
	ansibleGenerator ansible.JobGenerator

	jobControl controller.JobControl

	jobSync controller.JobSync

	// used for unit testing
	enqueueCluster func(cluster metav1.Object)

	clusterLister    capilister.ClusterLister
	machineSetLister capilister.MachineSetLister

	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// jobsSynced returns true if the job shared informer has been synced at least once.
	jobsSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface

	Logger log.FieldLogger
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*capi.Cluster)
	logging.WithCluster(c.Logger, cluster).
		Debugf("Adding cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) updateCluster(old, cur interface{}) {
	cluster := cur.(*capi.Cluster)
	logging.WithCluster(c.Logger, cluster).
		Debugf("Updating cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*capi.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*capi.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	logging.WithCluster(c.Logger, cluster).
		Debugf("Deleting cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(*capi.MachineSet)
	if !isMasterMachineSet(machineSet) {
		return
	}
	logging.WithMachineSet(c.Logger, machineSet).
		Debugf("Adding master machine set")
	c.enqueueClusterForMachineSet(machineSet)
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	machineSet := cur.(*capi.MachineSet)
	if !isMasterMachineSet(machineSet) {
		return
	}
	logging.WithMachineSet(c.Logger, machineSet).
		Debugf("Updating master machine set")
	c.enqueueClusterForMachineSet(machineSet)
}

func (c *Controller) deleteMachineSet(obj interface{}) {
	machineSet, ok := obj.(*capi.MachineSet)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		machineSet, ok = tombstone.Obj.(*capi.MachineSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a MachineSet %#v", obj))
			return
		}
	}
	if !isMasterMachineSet(machineSet) {
		return
	}
	logging.WithMachineSet(c.Logger, machineSet).
		Debugf("Deleting master machine set")
	c.enqueueClusterForMachineSet(machineSet)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.Logger.Infof("Starting controller")
	defer c.Logger.Infof("Shutting down controller")

	if !controller.WaitForCacheSync(c.controllerName, stopCh, c.clustersSynced, c.machineSetsSynced, c.jobsSynced) {
		c.Logger.Errorf("Could not sync caches for controller")
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(cluster metav1.Object) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

// enqueueAfter will enqueue a cluster after the provided amount of time.
func (c *Controller) enqueueAfter(cluster metav1.Object, after time.Duration) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.AddAfter(key, after)
}

func (c *Controller) enqueueClusterForMachineSet(machineSet *capi.MachineSet) {
	cluster, err := controller.ClusterForMachineSet(machineSet, c.clusterLister)
	if err != nil {
		logging.WithMachineSet(c.Logger, machineSet).
			Warnf("Error getting cluster for master machine set: %v")
		return
	}
	if cluster == nil {
		logging.WithMachineSet(c.Logger, machineSet).
			Infof("No cluster for master machine set")
	}
	c.enqueueCluster(cluster)
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

	logger := c.Logger.WithField("cluster", key)

	if c.queue.NumRequeues(key) < maxRetries {
		logger.Infof("Error syncing cluster: %v", err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("Dropping cluster out of the queue: %v", err)
	c.queue.Forget(key)
}

func isMasterMachineSet(machineSet *capi.MachineSet) bool {
	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machineSet.Spec.Template.Spec)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't decode provider config from %#v: %v", machineSet, err))
		return false
	}
	return coMachineSetSpec.NodeType == clustop.NodeTypeMaster
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
	cluster, err := c.controller.clusterLister.Clusters(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return controller.ConvertToCombinedCluster(cluster)
}

func (c *jobOwnerControl) OnOwnedJobEvent(owner metav1.Object) {
	c.controller.enqueueCluster(owner)
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
	cluster, err := s.controller.clusterLister.Clusters(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return controller.ConvertToCombinedCluster(cluster)
}

func (s *jobSyncStrategy) DoesOwnerNeedProcessing(owner metav1.Object) bool {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.Logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return false
	}

	machineSet, err := controller.GetMasterMachineSet(cluster, s.controller.machineSetLister)
	if err != nil {
		logging.WithCombinedCluster(s.controller.Logger, cluster).
			Debugf("could not get master machine set: %v", err)
		return false
	}

	return s.controller.installStrategy.ReadyToInstall(cluster, machineSet)
}

func (s *jobSyncStrategy) GetJobFactory(owner metav1.Object, deleting bool) (controller.JobFactory, error) {
	if deleting {
		return nil, fmt.Errorf("should not be undoing on deletes")
	}

	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		return nil, fmt.Errorf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
	}

	jobGeneratorExecutor := ansible.NewJobGeneratorExecutorForMasterMachineSet(s.controller.ansibleGenerator, s.controller.playbooks, cluster, cluster.AWSClusterProviderConfig.OpenShiftConfig.Version)
	if decorateStrategy, shouldDecorate := s.controller.installStrategy.(InstallJobDecorationStrategy); shouldDecorate {
		decorateStrategy.DecorateJobGeneratorExecutor(jobGeneratorExecutor, cluster)
	}

	return jobFactory(func(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
		job, configMap, err := jobGeneratorExecutor.Execute(name)
		if err != nil {
			return nil, nil, err
		}
		labels := controller.JobLabelsForClusterController(cluster, s.controller.controllerName)
		controller.AddLabels(job, labels)
		controller.AddLabels(configMap, labels)
		return job, configMap, nil
	}), nil
}

func (s *jobSyncStrategy) DeepCopyOwner(owner metav1.Object) metav1.Object {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.Logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return cluster
	}
	return cluster.DeepCopy()
}

func (s *jobSyncStrategy) SetOwnerJobSyncCondition(
	owner metav1.Object,
	conditionType controller.JobSyncConditionType,
	status kapi.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck controller.UpdateConditionCheck,
) {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.Logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return
	}
	cluster.ClusterProviderStatus.Conditions = controller.SetClusterCondition(
		cluster.ClusterProviderStatus.Conditions,
		s.controller.installStrategy.ConvertJobSyncConditionType(conditionType),
		status,
		reason,
		message,
		updateConditionCheck,
	)
}

func (s *jobSyncStrategy) OnJobCompletion(owner metav1.Object, job *v1batch.Job, succeeded bool) {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.Logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return
	}
	machineSet, err := controller.GetMasterMachineSet(cluster, s.controller.machineSetLister)
	if err != nil {
		s.controller.Logger.Warnf("could not get the master machine set: %v", err)
		return
	}
	s.controller.installStrategy.OnInstall(succeeded, cluster, machineSet, job)

	if reprocessStrategy, shouldReprocess := s.controller.installStrategy.(controller.JobSyncReprocessStrategy); shouldReprocess {
		// We often sync before we hit this reprocess interval, if the threshold for
		// reprocessing happens to be exceeded on one of those syncs we may actually launch a
		// new job earlier here.
		reprocessDuration := wait.Jitter(reprocessStrategy.GetReprocessInterval(), 1.0)
		s.controller.enqueueAfter(cluster, reprocessDuration)
	}
}

func (s *jobSyncStrategy) UpdateOwnerStatus(original, owner metav1.Object) error {
	combinedCluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		return fmt.Errorf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
	}
	cluster, err := controller.ClusterAPIClusterForCombinedCluster(combinedCluster, false /*ignoreChanges*/)
	if err != nil {
		return err
	}
	return controller.UpdateClusterStatus(s.controller.capiClient, cluster)
}

type jobSyncWithReprocessStrategy struct {
	*jobSyncStrategy
}

var _ controller.JobSyncReprocessStrategy = (*jobSyncWithReprocessStrategy)(nil)

func (s *jobSyncWithReprocessStrategy) GetReprocessInterval() time.Duration {
	return s.controller.installStrategy.(controller.JobSyncReprocessStrategy).GetReprocessInterval()
}

func (s *jobSyncWithReprocessStrategy) GetLastJobSuccess(owner metav1.Object) *time.Time {
	cluster, ok := owner.(*clustop.CombinedCluster)
	if !ok {
		s.controller.Logger.Warn("could not convert owner from JobSync into a cluster: %#v", owner)
		return nil
	}
	return s.controller.installStrategy.(controller.JobSyncReprocessStrategy).GetLastJobSuccess(cluster)
}
