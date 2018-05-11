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

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	v1rbac "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
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

	"github.com/openshift/cluster-operator/pkg/ansible"
	clusterop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteropclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	clusteropinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	colog "github.com/openshift/cluster-operator/pkg/logging"
	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	masterPlaybook = "playbooks/cluster-operator/aws/install_masters.yml"
	// jobType is the type of job run by this controller.
	jobType = "master"

	serviceAccountName = "cluster-installer"
	roleBindingName    = "cluster-installer"

	// this name comes from the created ClusterRole in the deployment
	// template contrib/examples/deploy.yaml
	secretCreatorRoleName = "clusteroperator.openshift.io:master-controller"
)

// NewClustopController returns a new *Controller for cluster-operator
// resources.
func NewClustopController(
	clusterInformer clusteropinformers.ClusterInformer,
	machineSetInformer clusteropinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clusteropclientset.Interface,
) *Controller {
	clusterLister := clusterInformer.Lister()
	machineSetLister := machineSetInformer.Lister()
	return newController(
		clusterop.SchemeGroupVersion.WithKind("Cluster"),
		"clusteroperator_master_controller",
		"master",
		clustopClient,
		nil,
		kubeClient,
		jobInformer,
		func(handler cache.ResourceEventHandler) { clusterInformer.Informer().AddEventHandler(handler) },
		func(handler cache.ResourceEventHandler) { machineSetInformer.Informer().AddEventHandler(handler) },
		func(namespace, name string) (metav1.Object, error) {
			return clusterLister.Clusters(namespace).Get(name)
		},
		func(namespace, name string) (metav1.Object, error) {
			return machineSetLister.MachineSets(namespace).Get(name)
		},
		clusterInformer.Informer().HasSynced,
		machineSetInformer.Informer().HasSynced,
	)
}

// NewCAPIController returns a new *Controller for cluster-api resources.
func NewCAPIController(
	clusterInformer capiinformers.ClusterInformer,
	machineSetInformer capiinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clusteropclientset.Interface,
	capiClient capiclientset.Interface,
) *Controller {
	clusterLister := clusterInformer.Lister()
	machineSetLister := machineSetInformer.Lister()
	return newController(
		capi.SchemeGroupVersion.WithKind("Cluster"),
		"clusteroperator_capi_master_controller",
		"capi-master",
		clustopClient,
		capiClient,
		kubeClient,
		jobInformer,
		func(handler cache.ResourceEventHandler) { clusterInformer.Informer().AddEventHandler(handler) },
		func(handler cache.ResourceEventHandler) { machineSetInformer.Informer().AddEventHandler(handler) },
		func(namespace, name string) (metav1.Object, error) {
			return clusterLister.Clusters(namespace).Get(name)
		},
		func(namespace, name string) (metav1.Object, error) {
			return machineSetLister.MachineSets(namespace).Get(name)
		},
		clusterInformer.Informer().HasSynced,
		machineSetInformer.Informer().HasSynced,
	)
}

// newController returns a new *Controller.
func newController(
	clusterKind schema.GroupVersionKind,
	metricsName string,
	loggerName string,
	clustopClient clusteropclientset.Interface,
	capiClient capiclientset.Interface,
	kubeClient kubeclientset.Interface,
	jobInformer batchinformers.JobInformer,
	addClusterInformerEventHandler func(cache.ResourceEventHandler),
	addMachineSetInformerEventHandler func(cache.ResourceEventHandler),
	getCluster func(namespace, name string) (metav1.Object, error),
	getMachineSet func(namespace, name string) (metav1.Object, error),
	clustersSynced cache.InformerSynced,
	machineSetsSynced cache.InformerSynced,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage(metricsName, kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", loggerName)

	c := &Controller{
		clusterKind:       clusterKind,
		clustopClient:     clustopClient,
		capiClient:        capiClient,
		kubeClient:        kubeClient,
		queue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), loggerName),
		logger:            logger,
		getCluster:        getCluster,
		getMachineSet:     getMachineSet,
		clustersSynced:    clustersSynced,
		machineSetsSynced: machineSetsSynced,
	}

	addClusterInformerEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	addMachineSetInformerEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		DeleteFunc: c.deleteMachineSet,
	})

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(jobType, clusterKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	c.jobSync = controller.NewJobSync(c.jobControl, &jobSyncStrategy{controller: c}, false, logger)

	c.syncHandler = c.jobSync.Sync
	c.enqueueCluster = c.enqueue
	c.ansibleGenerator = ansible.NewJobGenerator()

	return c
}

// Controller manages launching the master/control plane on machines
// that are masters in the cluster.
type Controller struct {
	clusterKind schema.GroupVersionKind

	clustopClient clusteropclientset.Interface
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

	// getCluster gets the cluster with the specified namespace and name from
	// the lister
	getCluster func(namespace, name string) (metav1.Object, error)
	// getMachineSet gets the machineset with the specified namespace and name
	// from the lister
	getMachineSet func(namespace, name string) (metav1.Object, error)
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

	logger log.FieldLogger
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(metav1.Object)
	colog.WithGenericCluster(c.logger, cluster).
		Debugf("Adding cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) updateCluster(old, cur interface{}) {
	cluster := cur.(metav1.Object)
	colog.WithGenericCluster(c.logger, cluster).
		Debugf("Updating cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(metav1.Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	colog.WithGenericCluster(c.logger, cluster).
		Debugf("Deleting cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(metav1.Object)
	if !isMasterMachineSet(machineSet) {
		return
	}
	colog.WithGenericMachineSet(c.logger, machineSet).
		Debugf("Adding master machine set")
	c.enqueueClusterForMachineSet(machineSet)
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	machineSet := cur.(metav1.Object)
	if !isMasterMachineSet(machineSet) {
		return
	}
	colog.WithGenericMachineSet(c.logger, machineSet).
		Debugf("Updating master machine set")
	c.enqueueClusterForMachineSet(machineSet)
}

func (c *Controller) deleteMachineSet(obj interface{}) {
	machineSet, ok := obj.(metav1.Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		machineSet, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a MachineSet %#v", obj))
			return
		}
	}
	if !isMasterMachineSet(machineSet) {
		return
	}
	colog.WithGenericMachineSet(c.logger, machineSet).
		Debugf("Deleting master machine set")
	c.enqueueClusterForMachineSet(machineSet)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("Starting master controller")
	defer c.logger.Infof("Shutting down master controller")

	if !controller.WaitForCacheSync("master", stopCh, c.clustersSynced, c.machineSetsSynced, c.jobsSynced) {
		c.logger.Errorf("Could not sync caches for master controller")
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

func (c *Controller) enqueueClusterForMachineSet(machineSet metav1.Object) {
	cluster, err := controller.ClusterForGenericMachineSet(machineSet, c.clusterKind, c.getCluster)
	if err != nil {
		colog.WithGenericMachineSet(c.logger, machineSet).
			Warnf("Error getting cluster for master machine set: %v")
		return
	}
	if cluster == nil {
		colog.WithGenericMachineSet(c.logger, machineSet).
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

	logger := c.logger.WithField("cluster", key)

	if c.queue.NumRequeues(key) < maxRetries {
		logger.Infof("Error syncing cluster: %v", err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("Dropping cluster out of the queue: %v", err)
	c.queue.Forget(key)
}

func isMasterMachineSet(obj metav1.Object) bool {
	switch machineSet := obj.(type) {
	case *clusterop.MachineSet:
		return machineSet.Spec.NodeType == clusterop.NodeTypeMaster
	case *capi.MachineSet:
		for _, role := range machineSet.Spec.Template.Spec.Roles {
			if role == capicommon.MasterRole {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (c *Controller) getMasterMachineSet(cluster *clusterop.CombinedCluster) (metav1.Object, error) {
	masterMachineSetName := cluster.ClusterOperatorStatus.MasterMachineSetName
	if masterMachineSetName == "" {
		return nil, fmt.Errorf("no master machine set established")
	}
	return c.getMachineSet(cluster.Namespace, masterMachineSetName)
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
	cluster, err := c.controller.getCluster(namespace, name)
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
		_, err := s.controller.clustopClient.ClusteroperatorV1alpha1().
			ClusterVersions(cluster.ClusterOperatorSpec.ClusterVersionRef.Namespace).
			Get(cluster.ClusterOperatorSpec.ClusterVersionRef.Name, metav1.GetOptions{})
		if err != nil {
			return false
		}
	}

	machineSet, err := s.controller.getMasterMachineSet(cluster)
	if err != nil {
		colog.WithGenericCluster(s.controller.logger, cluster).
			Debugf("could not get master machine set: %v", err)
		return false
	}

	return cluster.ClusterOperatorStatus.ControlPlaneInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterOperatorStatus.ControlPlaneInstalledJobMachineSetGeneration != machineSet.GetGeneration()
}

func (s *jobSyncStrategy) GetReprocessInterval() *time.Duration {
	return nil
}

func (s *jobSyncStrategy) GetLastJobSuccess(owner metav1.Object) *time.Time {
	return nil
}

func (s *jobSyncStrategy) GetJobFactory(owner metav1.Object, deleting bool) (controller.JobFactory, error) {
	if deleting {
		return nil, fmt.Errorf("should not be undoing on deletes")
	}

	cluster, err := controller.ConvertToCombinedCluster(owner)
	if err != nil {
		return nil, fmt.Errorf("could not convert owner from JobSync into a cluster: %v: %#v", err, owner)
	}

	serviceAccount, err := s.setupServiceAccountForJob(owner.GetNamespace())
	if err != nil {
		return nil, fmt.Errorf("error creating or setting up service account %v", err)
	}

	return jobFactory(func(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
		cvRef := cluster.ClusterOperatorSpec.ClusterVersionRef
		cv, err := s.controller.clustopClient.Clusteroperator().ClusterVersions(cvRef.Namespace).Get(cvRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}

		vars, err := ansible.GenerateClusterWideVarsForMachineSet(true /*isMaster*/, cluster.Name, &cluster.ClusterOperatorSpec.Hardware, cv)
		if err != nil {
			return nil, nil, err
		}
		image, pullPolicy := ansible.GetAnsibleImageForClusterVersion(cv)
		job, configMap := s.controller.ansibleGenerator.GeneratePlaybookJobWithServiceAccount(
			name,
			&cluster.ClusterOperatorSpec.Hardware,
			masterPlaybook,
			ansible.DefaultInventory,
			vars,
			image,
			pullPolicy,
			serviceAccount,
		)
		labels := controller.JobLabelsForClusterController(cluster, jobType)
		controller.AddLabels(job, labels)
		controller.AddLabels(configMap, labels)
		return job, configMap, nil
	}), nil
}

// create a serviceaccount and rolebinding so that the master job
// can save the target cluster's kubeconfig
func (s *jobSyncStrategy) setupServiceAccountForJob(clusterNamespace string) (*kapi.ServiceAccount, error) {
	// create new serviceaccount if it doesn't already exist
	currentSA, err := s.controller.kubeClient.Core().ServiceAccounts(clusterNamespace).Get(serviceAccountName, metav1.GetOptions{})
	if err != nil && !apierrs.IsNotFound(err) {
		return nil, fmt.Errorf("error checking for existing serviceaccount")
	}

	if apierrs.IsNotFound(err) {
		currentSA, err = s.controller.kubeClient.Core().ServiceAccounts(clusterNamespace).Create(&kapi.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: serviceAccountName,
			},
		})

		if err != nil {
			return nil, fmt.Errorf("error creating serviceaccount for master job")
		}
	}
	s.controller.logger.Debugf("using serviceaccount: %+v", currentSA)

	// create rolebinding for the serviceaccount
	currentRB, err := s.controller.kubeClient.Rbac().RoleBindings(clusterNamespace).Get(roleBindingName, metav1.GetOptions{})

	if err != nil && !apierrs.IsNotFound(err) {
		return nil, fmt.Errorf("error checking for exising rolebinding")
	}

	if apierrs.IsNotFound(err) {
		rb := &v1rbac.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleBindingName,
				Namespace: clusterNamespace,
			},
			Subjects: []v1rbac.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      currentSA.Name,
					Namespace: clusterNamespace,
				},
			},
			RoleRef: v1rbac.RoleRef{
				Name: secretCreatorRoleName,
				Kind: "ClusterRole",
			},
		}

		currentRB, err = s.controller.kubeClient.Rbac().RoleBindings(clusterNamespace).Create(rb)
		if err != nil {
			return nil, fmt.Errorf("couldn't create rolebinding to clusterrole")
		}
	}
	s.controller.logger.Debugf("rolebinding to serviceaccount: %+v", currentRB)

	return currentSA, nil

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
	machineSet, err := s.controller.getMasterMachineSet(cluster)
	if err != nil {
		s.controller.logger.Warnf("could not get the master machine set: %v", err)
		return
	}
	cluster.ClusterOperatorStatus.ControlPlaneInstalled = succeeded
	cluster.ClusterOperatorStatus.ControlPlaneInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterOperatorStatus.ControlPlaneInstalledJobMachineSetGeneration = machineSet.GetGeneration()
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
	case clusterop.SchemeGroupVersion.Group:
		originalCluster := controller.ClusterOperatorClusterForCombinedCluster(originalCombinedCluster)
		cluster := controller.ClusterOperatorClusterForCombinedCluster(combinedCluster)
		return controller.PatchClusterStatus(s.controller.clustopClient, originalCluster, cluster)
	case capi.SchemeGroupVersion.Group:
		originalCluster, err := controller.ClusterAPIClusterForCombinedCluster(originalCombinedCluster, true /*ignoreChanges*/)
		if err != nil {
			return err
		}
		cluster, err := controller.ClusterAPIClusterForCombinedCluster(combinedCluster, false /*ignoreChanges*/)
		if err != nil {
			return err
		}
		return controller.PatchClusterAPIStatus(s.controller.capiClient, originalCluster, cluster)
	default:
		return fmt.Errorf("unknown cluster kind %+v", s.controller.clusterKind)
	}
}

func convertJobSyncConditionType(conditionType controller.JobSyncConditionType) clusterop.ClusterConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clusterop.ControlPlaneInstalling
	case controller.JobSyncProcessed:
		return clusterop.ControlPlaneInstalled
	case controller.JobSyncProcessingFailed:
		return clusterop.ControlPlaneInstallationFailed
	default:
		return clusterop.ClusterConditionType("")
	}
}
