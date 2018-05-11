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

package infra

import (
	"fmt"
	"time"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	"github.com/openshift/cluster-operator/pkg/controller"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	clusteroperatorinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"

	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	clusterapiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	infraPlaybook            = "playbooks/cluster-operator/aws/infrastructure.yml"
	deprovisionInfraPlaybook = "playbooks/cluster-operator/aws/uninstall_infrastructure.yml"
)

// NewClusterOperatorController returns a new *Controller to use with
// cluster-operator resources.
func NewClusterOperatorController(
	clusterInformer clusteroperatorinformers.ClusterInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient clusteroperatorclientset.Interface,
) *Controller {
	clusterLister := clusterInformer.Lister()
	return newController(
		clusteroperator.SchemeGroupVersion.WithKind("Cluster"),
		"infra",
		clusteroperatorClient,
		nil,
		kubeClient,
		jobInformer,
		func(handler cache.ResourceEventHandler) { clusterInformer.Informer().AddEventHandler(handler) },
		func(namespace, name string) (metav1.Object, error) {
			return clusterLister.Clusters(namespace).Get(name)
		},
		clusterInformer.Informer().HasSynced,
	)
}

// NewClusterAPIController returns a new *Controller to use with
// cluster-api resources.
func NewClusterAPIController(
	clusterInformer clusterapiinformers.ClusterInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient clusteroperatorclientset.Interface,
	clusterapiClient clusterapiclientset.Interface,
) *Controller {
	clusterLister := clusterInformer.Lister()
	return newController(
		clusterapi.SchemeGroupVersion.WithKind("Cluster"),
		"capi-infra",
		clusteroperatorClient,
		clusterapiClient,
		kubeClient,
		jobInformer,
		func(handler cache.ResourceEventHandler) { clusterInformer.Informer().AddEventHandler(handler) },
		func(namespace, name string) (metav1.Object, error) {
			return clusterLister.Clusters(namespace).Get(name)
		},
		clusterInformer.Informer().HasSynced,
	)
}

func newController(
	clusterKind schema.GroupVersionKind,
	controllerName string,
	clusteroperatorClient clusteroperatorclientset.Interface,
	clusterapiClient clusterapiclientset.Interface,
	kubeClient kubeclientset.Interface,
	jobInformer batchinformers.JobInformer,
	addInformerEventHandler func(cache.ResourceEventHandler),
	getCluster func(namespace, name string) (metav1.Object, error),
	clustersSynced cache.InformerSynced,
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
		clusterKind:    clusterKind,
		controllerName: controllerName,
		coClient:       clusteroperatorClient,
		caClient:       clusterapiClient,
		kubeClient:     kubeClient,
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName),
		logger:         logger,
		getCluster:     getCluster,
		clustersSynced: clustersSynced,
	}

	addInformerEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(controllerName, clusterKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	c.jobSync = controller.NewJobSync(c.jobControl, &jobSyncStrategy{controller: c}, true, logger)

	c.syncHandler = c.jobSync.Sync
	c.enqueueCluster = c.enqueue
	c.ansibleGenerator = ansible.NewJobGenerator()

	return c
}

// Controller manages clusters.
type Controller struct {
	clusterKind    schema.GroupVersionKind
	controllerName string

	coClient   clusteroperatorclientset.Interface
	caClient   clusterapiclientset.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error

	// To allow injection of mock ansible generator for testing
	ansibleGenerator ansible.JobGenerator

	jobControl controller.JobControl

	jobSync controller.JobSync

	// used for unit testing
	enqueueCluster func(cluster metav1.Object)

	// getCluster gets the cluster with the specified namespace and name from
	// the lister
	getCluster func(namespace, name string) (metav1.Object, error)
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// jobsSynced returns true if the job shared informer has been synced at least once.
	jobsSynced cache.InformerSynced

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	logger *log.Entry
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(metav1.Object)
	c.logger.Debugf("enqueueing added cluster %s/%s", cluster.GetNamespace(), cluster.GetName())
	c.enqueueCluster(cluster)
}

func (c *Controller) updateCluster(old, obj interface{}) {
	cluster := obj.(metav1.Object)
	c.logger.Debugf("enqueueing updated cluster %s/%s", cluster.GetNamespace(), cluster.GetName())
	c.enqueueCluster(cluster)
}

func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(metav1.Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not an Object %#v", obj))
			return
		}
	}
	c.logger.Debugf("enqueueing deleted cluster %s/%s", cluster.GetNamespace(), cluster.GetName())
	c.enqueueCluster(cluster)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("starting infra controller")
	defer c.logger.Infof("shutting down infra controller")

	if !controller.WaitForCacheSync("infra", stopCh, c.clustersSynced, c.jobsSynced) {
		c.logger.Errorf("Could not sync caches for infra controller")
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
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", cluster, err))
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

	logger := c.logger.WithField("cluster", key)

	logger.Errorf("error syncing cluster: %v", err)
	if c.queue.NumRequeues(key) < maxRetries {
		logger.Errorf("retrying cluster")
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("dropping cluster out of the queue: %v", err)
	c.queue.Forget(key)
}

type jobOwnerControl struct {
	controller *Controller
}

func (c *jobOwnerControl) GetOwnerKey(owner metav1.Object) (string, error) {
	return controller.KeyFunc(owner)
}

func (c *jobOwnerControl) GetOwner(namespace string, name string) (metav1.Object, error) {
	cluster, err := c.controller.getCluster(namespace, name)
	if err != nil {
		return nil, err
	}
	return controller.ConvertToCombinedCluster(cluster)
}

func (c *jobOwnerControl) OnOwnedJobEvent(owner metav1.Object) {
	c.controller.enqueueCluster(owner)
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
	cluster, err := s.controller.getCluster(namespace, name)
	if err != nil {
		return nil, err
	}
	return controller.ConvertToCombinedCluster(cluster)
}

func (s *jobSyncStrategy) DoesOwnerNeedProcessing(owner metav1.Object) bool {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return false
	}

	// cannot run ansible jobs until the ClusterVersion has been resolved
	if cluster.ClusterOperatorStatus.ClusterVersionRef == nil {
		// staebler: Temporary work-around until we have a controller that sets
		// the ClusterVersionRef for cluster-api clusters.
		if cluster.ClusterAPISpec == nil {
			return false
		}
		_, err := s.controller.coClient.ClusteroperatorV1alpha1().
			ClusterVersions(cluster.ClusterOperatorSpec.ClusterVersionRef.Namespace).
			Get(cluster.ClusterOperatorSpec.ClusterVersionRef.Name, metav1.GetOptions{})
		if err != nil {
			return false
		}
	}

	return cluster.ClusterOperatorStatus.ProvisionedJobGeneration != cluster.Generation
}

func (s *jobSyncStrategy) GetJobFactory(owner metav1.Object, deleting bool) (controller.JobFactory, error) {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		return nil, fmt.Errorf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
	}
	playbook := infraPlaybook
	if deleting {
		playbook = deprovisionInfraPlaybook
	}
	jobFactory := jobFactory(func(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
		cvRef := cluster.ClusterOperatorSpec.ClusterVersionRef
		cv, err := s.controller.coClient.Clusteroperator().ClusterVersions(cvRef.Namespace).Get(cvRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		vars, err := ansible.GenerateClusterVars(cluster.Name, cluster.ClusterOperatorSpec, &cv.Spec)
		if err != nil {
			return nil, nil, err
		}
		image, pullPolicy := ansible.GetAnsibleImageForClusterVersion(cv)
		job, configMap := s.controller.ansibleGenerator.GeneratePlaybookJob(name, &cluster.ClusterOperatorSpec.Hardware, playbook, ansible.DefaultInventory, vars, image, pullPolicy)
		labels := controller.JobLabelsForClusterController(cluster, s.controller.controllerName)
		controller.AddLabels(job, labels)
		controller.AddLabels(configMap, labels)
		return job, configMap, nil
	})
	return jobFactory, nil
}

func (s *jobSyncStrategy) DeepCopyOwner(owner metav1.Object) metav1.Object {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
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
		s.controller.logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return
	}
	controller.SetClusterCondition(
		cluster.ClusterOperatorStatus,
		convertJobSyncConditionType(conditionType),
		status,
		reason,
		message,
		updateConditionCheck,
	)
}

func (s *jobSyncStrategy) OnJobCompletion(owner metav1.Object, job *v1batch.Job, succeeded bool) {
	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		s.controller.logger.Warnf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
		return
	}
	cluster.ClusterOperatorStatus.Provisioned = succeeded
	cluster.ClusterOperatorStatus.ProvisionedJobGeneration = cluster.Generation
}

func (s *jobSyncStrategy) UpdateOwnerStatus(original, owner metav1.Object) error {
	originalCombinedCluster, err := controller.ConvertToCombinedCluster(original)
	if err != nil {
		return fmt.Errorf("could not convert original from JobSync into a cluster: %v: %#v", err, owner)
	}
	combinedCluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		return fmt.Errorf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
	}
	switch s.controller.clusterKind.Group {
	case clusteroperator.SchemeGroupVersion.Group:
		originalCluster := controller.ClusterOperatorClusterForCombinedCluster(originalCombinedCluster)
		cluster := controller.ClusterOperatorClusterForCombinedCluster(combinedCluster)
		return controller.PatchClusterStatus(s.controller.coClient, originalCluster, cluster)
	case clusterapi.SchemeGroupVersion.Group:
		originalCluster, err := controller.ClusterAPIClusterForCombinedCluster(originalCombinedCluster, true /*ignoreChanges*/)
		if err != nil {
			return err
		}
		cluster, err := controller.ClusterAPIClusterForCombinedCluster(combinedCluster, false /*ignoreChanges*/)
		if err != nil {
			return err
		}
		return controller.PatchClusterAPIStatus(s.controller.caClient, originalCluster, cluster)
	default:
		return fmt.Errorf("unknown cluster kind %+v", s.controller.clusterKind)
	}
}

func (s *jobSyncStrategy) CanUndo(owner metav1.Object) bool {
	cluster, ok := owner.(*clusteroperator.CombinedCluster)
	if !ok {
		return false
	}
	return cluster.ClusterOperatorStatus.DeprovisionedComputeMachinesets
}

func convertJobSyncConditionType(conditionType controller.JobSyncConditionType) clusteroperator.ClusterConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clusteroperator.ClusterInfraProvisioning
	case controller.JobSyncProcessed:
		return clusteroperator.ClusterInfraProvisioned
	case controller.JobSyncProcessingFailed:
		return clusteroperator.ClusterInfraProvisioningFailed
	case controller.JobSyncUndoing:
		return clusteroperator.ClusterInfraDeprovisioning
	case controller.JobSyncUndoFailed:
		return clusteroperator.ClusterInfraDeprovisioningFailed
	default:
		return clusteroperator.ClusterConditionType("")
	}
}
