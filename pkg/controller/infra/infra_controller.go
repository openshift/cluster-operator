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

package infra

import (
	"fmt"
	"time"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/cluster-operator/pkg/ansible"
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

	controllerLogName = "infra"

	infraPlaybook = "playbooks/cluster-operator/aws/infrastructure.yml"
	deprovisionInfraPlaybook = "playbooks/aws/openshift-cluster/uninstall_prerequisites.yml"
	// jobPrefix is used when generating a name for the configmap and job used for each
	// Ansible execution.
	jobPrefix = "job-infra-"
)

const provisionInventoryTemplate = `
[OSEv3:children]
masters
nodes
etcd

[OSEv3:vars]

[masters]

[etcd]

[nodes]
`

var clusterKind = clusteroperator.SchemeGroupVersion.WithKind("Cluster")

// NewInfraController returns a new *InfraController.
func NewInfraController(
	clusterInformer informers.ClusterInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient clusteroperatorclientset.Interface,
	ansibleImage string,
	ansibleImagePullPolicy kapi.PullPolicy,
) *InfraController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_cluster_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)
	c := &InfraController{
		coClient:   clusteroperatorClient,
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),
		logger:     logger,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	jobOwnerControl := &jobOwnerControl{controller: c}
	c.jobControl = controller.NewJobControl(jobPrefix, clusterKind, kubeClient, jobInformer.Lister(), jobOwnerControl, logger)
	jobInformer.Informer().AddEventHandler(c.jobControl)
	c.jobsSynced = jobInformer.Informer().HasSynced

	c.syncHandler = c.syncCluster
	c.enqueueCluster = c.enqueue
	c.ansibleGenerator = ansible.NewJobGenerator(ansibleImage, ansibleImagePullPolicy)

	return c
}

// InfraController manages clusters.
type InfraController struct {
	coClient   clusteroperatorclientset.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error

	// To allow injection of mock ansible generator for testing
	ansibleGenerator ansible.JobGenerator

	jobControl controller.JobControl

	// used for unit testing
	enqueueCluster func(cluster *clusteroperator.Cluster)

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewInfraController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// jobsSynced returns true of the job shared informer has been synced at least once.
	jobsSynced cache.InformerSynced

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	logger *log.Entry
}

func (c *InfraController) addCluster(obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	c.logger.Debugf("enqueueing added cluster %s/%s", cluster.Namespace, cluster.Name)
	c.enqueueCluster(cluster)
}

func (c *InfraController) updateCluster(old, obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	c.logger.Debugf("enqueueing updated cluster %s/%s", cluster.Namespace, cluster.Name)
	c.enqueueCluster(cluster)
}

func (c *InfraController) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*clusteroperator.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*clusteroperator.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a cluster %#v", obj))
			return
		}
	}
	c.logger.Debugf("enqueueing deleted cluster %s/%s", cluster.Namespace, cluster.Name)
	c.enqueueCluster(cluster)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// clusters will be handled in parallel.
func (c *InfraController) Run(workers int, stopCh <-chan struct{}) {
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

func (c *InfraController) enqueue(cluster *clusteroperator.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *InfraController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *InfraController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *InfraController) handleErr(err error, key interface{}) {
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

func (c *InfraController) syncClusterStatusWithJob(original *clusteroperator.Cluster, job *v1batch.Job) error {
	cluster := original.DeepCopy()
	now := metav1.Now()

	jobCompleted := jobCondition(job, v1batch.JobComplete)
	jobFailed := jobCondition(job, v1batch.JobFailed)
	switch {
	case jobCompleted != nil && jobCompleted.Status == kapi.ConditionTrue:
		clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioning)
		if clusterProvisioning != nil &&
			clusterProvisioning.Status == kapi.ConditionTrue {
			clusterProvisioning.Status = kapi.ConditionFalse
			clusterProvisioning.LastTransitionTime = now
			clusterProvisioning.LastProbeTime = now
			clusterProvisioning.Reason = "JobCompleted"
			clusterProvisioning.Message = fmt.Sprintf("Job %s/%s completed at %v", job.Namespace, job.Name, jobCompleted.LastTransitionTime)
		}
		clusterProvisioned := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioned)
		if clusterProvisioned != nil &&
			clusterProvisioned.Status == kapi.ConditionFalse {
			clusterProvisioned.Status = kapi.ConditionTrue
			clusterProvisioned.LastTransitionTime = now
			clusterProvisioned.LastProbeTime = now
			clusterProvisioned.Reason = "JobCompleted"
			clusterProvisioning.Message = fmt.Sprintf("Job %s/%s completed at %v", job.Namespace, job.Name, jobCompleted.LastTransitionTime)
		}
		if clusterProvisioned == nil {
			cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
				Type:               clusteroperator.ClusterInfraProvisioned,
				Status:             kapi.ConditionTrue,
				LastProbeTime:      now,
				LastTransitionTime: now,
				Reason:             "JobCompleted",
				Message:            fmt.Sprintf("Job %s/%s completed at %v", job.Namespace, job.Name, jobCompleted.LastTransitionTime),
			})
		}
		provisioningFailed := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioningFailed)
		if provisioningFailed != nil &&
			provisioningFailed.Status == kapi.ConditionTrue {
			provisioningFailed.Status = kapi.ConditionFalse
			provisioningFailed.LastTransitionTime = now
			provisioningFailed.LastProbeTime = now
			provisioningFailed.Reason = ""
			provisioningFailed.Message = ""
		}
		cluster.Status.Provisioned = true
		cluster.Status.ProvisionedJobGeneration = cluster.Generation
	case jobFailed != nil && jobFailed.Status == kapi.ConditionTrue:
		clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioning)
		if clusterProvisioning != nil &&
			clusterProvisioning.Status == kapi.ConditionTrue {
			clusterProvisioning.Status = kapi.ConditionFalse
			clusterProvisioning.LastTransitionTime = now
			clusterProvisioning.LastProbeTime = now
			clusterProvisioning.Reason = "JobFailed"
			clusterProvisioning.Message = fmt.Sprintf("Job %s/%s failed at %v, reason: %s", job.Namespace, job.Name, jobFailed.LastTransitionTime, jobFailed.Reason)
		}
		provisioningFailed := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioningFailed)
		if provisioningFailed != nil {
			provisioningFailed.Status = kapi.ConditionTrue
			provisioningFailed.LastTransitionTime = now
			provisioningFailed.LastProbeTime = now
			provisioningFailed.Reason = "JobFailed"
			provisioningFailed.Message = fmt.Sprintf("Job %s/%s failed at %v, reason: %s", job.Namespace, job.Name, jobFailed.LastTransitionTime, jobFailed.Reason)
		} else {
			cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
				Type:               clusteroperator.ClusterInfraProvisioningFailed,
				Status:             kapi.ConditionTrue,
				LastProbeTime:      now,
				LastTransitionTime: now,
				Reason:             "JobFailed",
				Message:            fmt.Sprintf("Job %s/%s failed at %v, reason: %s", job.Namespace, job.Name, jobFailed.LastTransitionTime, jobFailed.Reason),
			})
		}
		cluster.Status.ProvisionedJobGeneration = cluster.Generation
	default:
		clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioning)
		reason := "JobRunning"
		message := fmt.Sprintf("Job %s/%s is running since %v. Pod completions: %d, failures: %d", job.Namespace, job.Name, job.Status.StartTime, job.Status.Succeeded, job.Status.Failed)
		if clusterProvisioning != nil {
			if clusterProvisioning.Status != kapi.ConditionTrue {
				clusterProvisioning.Status = kapi.ConditionTrue
				clusterProvisioning.LastTransitionTime = now
				clusterProvisioning.LastProbeTime = now
				clusterProvisioning.Reason = reason
				clusterProvisioning.Message = message
			}
		} else {
			cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
				Type:               clusteroperator.ClusterInfraProvisioning,
				Status:             kapi.ConditionTrue,
				LastProbeTime:      now,
				LastTransitionTime: now,
				Reason:             reason,
				Message:            message,
			})
		}
	}

	return c.updateClusterStatus(original, cluster)
}

func (c *InfraController) updateClusterStatus(original, cluster *clusteroperator.Cluster) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return controller.PatchClusterStatus(c.coClient, original, cluster)
	})
}

// syncCluster will sync the cluster with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *InfraController) syncCluster(key string) error {
	startTime := time.Now()
	cLog := c.logger.WithField("cluster", key)
	cLog.Debugln("started syncing cluster")
	defer func() {
		cLog.WithField("duration", time.Since(startTime)).Debugln("finished syncing cluster")
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid cluster key %q: either namespace or name is missing", key)
	}

	cluster, err := c.clustersLister.Clusters(ns).Get(name)
	if errors.IsNotFound(err) {
		cLog.Debugln("cluster deleted")
		c.jobControl.ObserveOwnerDeletion(key)
		return nil
	}
	if err != nil {
		return err
	}

	// Are we dealing with a cluster marked for deletion
	if cluster.DeletionTimestamp != nil {
		c.logger.Debugf("DeletionTimestamp set on cluster %s", cluster.Name)
		cluster_copy := cluster.DeepCopy()
		finalizers := sets.NewString(cluster_copy.ObjectMeta.Finalizers...)

		if finalizers.Has(clusteroperator.FinalizerClusterOperator) {
			// Clear the finalizer for the cluster
			finalizers.Delete(clusteroperator.FinalizerClusterOperator)
			cluster_copy.ObjectMeta.Finalizers = finalizers.List()
			c.updateClusterStatus(cluster, cluster_copy)

			return c.runDeprovisionJob(cluster)
		}
		return nil
	}
	specChanged := cluster.Status.ProvisionedJobGeneration != cluster.Generation

	jobFactory := c.getProvisionJobFactory(cluster)

	job, isJobNew, err := c.jobControl.ControlJobs(key, cluster, specChanged, jobFactory)
	if err != nil {
		return err
	}

	if !specChanged {
		return nil
	}

	switch {
	// New job has not been created, so an old job must exist. Set the cluster
	// to not provisioning yet as the old job is deleted.
	case job == nil:
		return c.setClusterToNotProvisioning(cluster)
	// Job was not newly created, so sync cluster status with job.
	case !isJobNew:
		cLog.Debugln("provisioning job exists, will sync with job")
		return c.syncClusterStatusWithJob(cluster, job)
	// Cluster should have a job to provision the current spec but it was not
	// found.
	case isClusterProvisioning(cluster):
		return c.setJobNotFoundStatus(cluster)
	// New job created for new provisioning
	default:
		return nil
	}
}

func (c *InfraController) setClusterToNotProvisioning(original *clusteroperator.Cluster) error {
	cluster := original.DeepCopy()
	now := metav1.Now()

	clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioning)
	if clusterProvisioning != nil &&
		clusterProvisioning.Status == kapi.ConditionTrue {
		clusterProvisioning.Status = kapi.ConditionFalse
		clusterProvisioning.LastTransitionTime = now
		clusterProvisioning.LastProbeTime = now
		clusterProvisioning.Reason = "SpecChanged"
		clusterProvisioning.Message = "Spec changed. New provisioning needed"
	}

	return c.updateClusterStatus(original, cluster)
}

func isClusterProvisioning(cluster *clusteroperator.Cluster) bool {
	provisioning := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioning)
	return provisioning != nil && provisioning.Status == kapi.ConditionTrue
}

func jobCondition(job *v1batch.Job, conditionType v1batch.JobConditionType) *v1batch.JobCondition {
	for i, condition := range job.Status.Conditions {
		if condition.Type == conditionType {
			return &job.Status.Conditions[i]
		}
	}
	return nil
}

func clusterCondition(cluster *clusteroperator.Cluster, conditionType clusteroperator.ClusterConditionType) *clusteroperator.ClusterCondition {
	for i, condition := range cluster.Status.Conditions {
		if condition.Type == conditionType {
			return &cluster.Status.Conditions[i]
		}
	}
	return nil
}

func (c *InfraController) setJobNotFoundStatus(original *clusteroperator.Cluster) error {
	cluster := original.DeepCopy()
	now := metav1.Now()
	reason := "JobMissing"
	message := "Provisioning job not found."
	if provisioning := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioning); provisioning != nil {
		provisioning.Status = kapi.ConditionFalse
		provisioning.Reason = reason
		provisioning.Message = message
		provisioning.LastTransitionTime = now
		provisioning.LastProbeTime = now
	}
	provisioningFailed := clusterCondition(cluster, clusteroperator.ClusterInfraProvisioningFailed)
	if provisioningFailed != nil {
		provisioningFailed.Status = kapi.ConditionTrue
		provisioningFailed.Reason = reason
		provisioningFailed.Message = message
		provisioningFailed.LastTransitionTime = now
		provisioningFailed.LastProbeTime = now
	} else {
		cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
			Type:               clusteroperator.ClusterInfraProvisioningFailed,
			Status:             kapi.ConditionTrue,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: now,
			LastProbeTime:      now,
		})
	}
	return c.updateClusterStatus(original, cluster)
}

type jobOwnerControl struct {
	controller *InfraController
}

func (c *jobOwnerControl) GetOwnerKey(owner metav1.Object) (string, error) {
	return controller.KeyFunc(owner)
}

func (c *jobOwnerControl) GetOwner(namespace string, name string) (metav1.Object, error) {
	return c.controller.clustersLister.Clusters(namespace).Get(name)
}

func (c *jobOwnerControl) OnOwnedJobEvent(owner metav1.Object) {
	cluster, ok := owner.(*clusteroperator.Cluster)
	if !ok {
		c.controller.logger.WithFields(log.Fields{"owner": owner.GetName(), "namespace": owner.GetNamespace()}).
			Errorf("attempt to enqueue owner that is not a cluster")
		return
	}
	c.controller.enqueueCluster(cluster)
}

type jobFactory func(string) (*v1batch.Job, *kapi.ConfigMap, error)

func (f jobFactory) BuildJob(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
	return f(name)
}

func (c *InfraController) getJobFactory(cluster *clusteroperator.Cluster, playbook string) controller.JobFactory {
	c.logger.Infof("PLAYBOOK: %s", playbook)
	return jobFactory(func(name string) (*v1batch.Job, *kapi.ConfigMap, error) {
		varsGenerator := ansible.NewVarsGenerator(cluster)
		vars, err := varsGenerator.GenerateVars()
		if err != nil {
			return nil, nil, err
		}
		job, configMap := c.ansibleGenerator.GeneratePlaybookJob(name, &cluster.Spec.Hardware, playbook, provisionInventoryTemplate, vars)
		return job, configMap, nil
	})
}

func (c *InfraController) getProvisionJobFactory(cluster *clusteroperator.Cluster) controller.JobFactory {
	 return c.getJobFactory(cluster, infraPlaybook)
}

func (c *InfraController) getDeprovisionJobFactory(cluster *clusteroperator.Cluster) controller.JobFactory {
	 return c.getJobFactory(cluster, deprovisionInfraPlaybook)
}

// fire-and-forget infra deprovision Job
func (c *InfraController) runDeprovisionJob(cluster *clusteroperator.Cluster) error {
	name := names.SimpleNameGenerator.GenerateName(fmt.Sprintf("infra-deprovision-%s-", cluster.Name))

	job, configMap, err := c.getDeprovisionJobFactory(cluster).BuildJob(name)
	if err != nil {
		return err
	}

	_, err = c.kubeClient.CoreV1().ConfigMaps(cluster.Namespace).Create(configMap)
	if err != nil {
		return err
	}
	_, err = c.kubeClient.BatchV1().Jobs(cluster.Namespace).Create(job)
	return err
}
