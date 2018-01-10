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
	"strconv"
	"strings"
	"time"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"

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

	infraPlaybook = "playbooks/aws/openshift-cluster/prerequisites.yml"
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
		coClient:     clusteroperatorClient,
		kubeClient:   kubeClient,
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),
		expectations: controller.NewUIDTrackingControllerExpectations(controller.NewControllerExpectations()),
		logger:       logger,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addJob,
		UpdateFunc: c.updateJob,
		DeleteFunc: c.deleteJob,
	})
	c.jobsSynced = jobInformer.Informer().HasSynced
	c.jobsLister = jobInformer.Lister()

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

	// A TTLCache of infra job creations we're expecting to see
	expectations *controller.UIDTrackingControllerExpectations

	// used for unit testing
	enqueueCluster func(cluster *clusteroperator.Cluster)

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewClusterController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// jobsLister is able to list/get jobs and is populated by the shared informer passed to
	// NewClusterController.
	jobsLister batchlisters.JobLister
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

func isController(cluster *clusteroperator.Cluster, job *v1batch.Job) bool {
	controllerRef := metav1.GetControllerOf(job)
	if controllerRef == nil {
		return false
	}
	if controllerRef.Kind != clusterKind.Kind {
		return false
	}
	return controllerRef.UID == cluster.UID
}

func isInfraJob(job *v1batch.Job) bool {
	controllerRef := metav1.GetControllerOf(job)
	if controllerRef == nil {
		return false
	}
	if controllerRef.Kind != clusterKind.Kind {
		return false
	}
	return strings.HasPrefix(job.Name, jobPrefix)
}

func isActive(job *v1batch.Job) bool {
	jobCompleted := jobCondition(job, v1batch.JobComplete)
	if jobCompleted != nil && jobCompleted.Status == kapi.ConditionTrue {
		return false
	}

	jobFailed := jobCondition(job, v1batch.JobFailed)
	if jobFailed != nil && jobFailed.Status == kapi.ConditionTrue {
		return false
	}
	return true
}

func jobClusterGeneration(job *v1batch.Job) int64 {
	if job.Annotations == nil {
		return 0
	}
	generationStr, ok := job.Annotations[clusteroperator.ClusterGenerationAnnotation]
	if !ok {
		return 0
	}
	generation, err := strconv.ParseInt(generationStr, 10, 64)
	if err != nil {
		return 0
	}
	return generation
}

func (c *InfraController) clusterForJob(job *v1batch.Job) (*clusteroperator.Cluster, error) {
	controllerRef := metav1.GetControllerOf(job)
	if controllerRef.Kind != clusterKind.Kind {
		return nil, nil
	}
	cluster, err := c.clustersLister.Clusters(job.Namespace).Get(controllerRef.Name)
	if err != nil {
		return nil, err
	}
	if cluster.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil, nil
	}
	return cluster, nil
}

func (c *InfraController) addJob(obj interface{}) {
	job := obj.(*v1batch.Job)
	if isInfraJob(job) {
		cluster, err := c.clusterForJob(job)
		if err != nil {
			c.logger.Errorf("Cannot retrieve cluster for job %s: %v", jobKey(job), err)
			utilruntime.HandleError(err)
			return
		}
		clusterKey, err := controller.KeyFunc(cluster)
		if err != nil {
			c.logger.Errorf("Cannot get cluster key for %s/%s: %v", cluster.Namespace, cluster.Name, err)
			utilruntime.HandleError(err)
			return
		}
		c.logger.Debug("job creation observed")
		c.expectations.CreationObserved(clusterKey)

		c.logger.Debugf("enqueueing cluster %s/%s for created infra job %s", cluster.Namespace, cluster.Name, jobKey(job))
		c.enqueueCluster(cluster)
	}
}

func (c *InfraController) updateJob(old, obj interface{}) {
	job := obj.(*v1batch.Job)
	if isInfraJob(job) {
		cluster, err := c.clusterForJob(job)
		if err != nil {
			c.logger.Errorf("Cannot retrieve cluster for job %s: %v", jobKey(job), err)
			utilruntime.HandleError(err)
			return
		}
		c.logger.Debugf("enqueueing cluster %s/%s for updated infra job %s", cluster.Namespace, cluster.Name, jobKey(job))
		c.enqueueCluster(cluster)
	}
}

func (c *InfraController) deleteJob(obj interface{}) {
	job, ok := obj.(*v1batch.Job)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		job, ok = tombstone.Obj.(*v1batch.Job)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Job %#v", obj))
			return
		}
	}
	if !isInfraJob(job) {
		return
	}
	cluster, err := c.clusterForJob(job)
	if err != nil || cluster == nil {
		utilruntime.HandleError(fmt.Errorf("could not get cluster for deleted job %s", jobKey(job)))
	}
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
		clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioning)
		if clusterProvisioning != nil &&
			clusterProvisioning.Status == kapi.ConditionTrue {
			clusterProvisioning.Status = kapi.ConditionFalse
			clusterProvisioning.LastTransitionTime = now
			clusterProvisioning.LastProbeTime = now
			clusterProvisioning.Reason = "JobCompleted"
			clusterProvisioning.Message = fmt.Sprintf("Job %s/%s completed at %v", jobKey(job), jobCompleted.LastTransitionTime)
		}
		clusterProvisioned := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioned)
		if clusterProvisioned != nil &&
			clusterProvisioned.Status == kapi.ConditionFalse {
			clusterProvisioned.Status = kapi.ConditionTrue
			clusterProvisioned.LastTransitionTime = now
			clusterProvisioned.LastProbeTime = now
			clusterProvisioned.Reason = "JobCompleted"
			clusterProvisioning.Message = fmt.Sprintf("Job %s completed at %v", jobKey(job), jobCompleted.LastTransitionTime)
		}
		if clusterProvisioned == nil {
			cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
				Type:               clusteroperator.ClusterHardwareProvisioned,
				Status:             kapi.ConditionTrue,
				LastProbeTime:      now,
				LastTransitionTime: now,
				Reason:             "JobCompleted",
				Message:            fmt.Sprintf("Job %s completed at %v", jobKey(job), jobCompleted.LastTransitionTime),
			})
		}
		provisioningFailed := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioningFailed)
		if provisioningFailed != nil &&
			provisioningFailed.Status == kapi.ConditionTrue {
			provisioningFailed.Status = kapi.ConditionFalse
			provisioningFailed.LastTransitionTime = now
			provisioningFailed.LastProbeTime = now
			provisioningFailed.Reason = ""
			provisioningFailed.Message = ""
		}
		cluster.Status.Provisioned = true
	case jobFailed != nil && jobFailed.Status == kapi.ConditionTrue:
		clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioning)
		if clusterProvisioning != nil &&
			clusterProvisioning.Status == kapi.ConditionTrue {
			clusterProvisioning.Status = kapi.ConditionFalse
			clusterProvisioning.LastTransitionTime = now
			clusterProvisioning.LastProbeTime = now
			clusterProvisioning.Reason = "JobFailed"
			clusterProvisioning.Message = fmt.Sprintf("Job %s failed at %v, reason: %s", jobKey(job), jobFailed.LastTransitionTime, jobFailed.Reason)
		}
		provisioningFailed := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioningFailed)
		if provisioningFailed != nil {
			provisioningFailed.Status = kapi.ConditionTrue
			provisioningFailed.LastTransitionTime = now
			provisioningFailed.LastProbeTime = now
			provisioningFailed.Reason = "JobFailed"
			provisioningFailed.Message = fmt.Sprintf("Job %s failed at %v, reason: %s", jobKey(job), jobFailed.LastTransitionTime, jobFailed.Reason)
		} else {
			cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
				Type:               clusteroperator.ClusterHardwareProvisioningFailed,
				Status:             kapi.ConditionTrue,
				LastProbeTime:      now,
				LastTransitionTime: now,
				Reason:             "JobFailed",
				Message:            fmt.Sprintf("Job %s failed at %v, reason: %s", jobKey(job), jobFailed.LastTransitionTime, jobFailed.Reason),
			})
		}
	default:
		clusterProvisioning := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioning)
		reason := "JobRunning"
		message := fmt.Sprintf("Job %s is running since %v. Pod completions: %d, failures: %d", jobKey(job), job.Status.StartTime, job.Status.Succeeded, job.Status.Failed)
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
				Type:               clusteroperator.ClusterHardwareProvisioning,
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
		cLog.Debugln("cluster not found")
		c.expectations.DeleteExpectations(key)
		return nil
	}
	if err != nil {
		return err
	}

	if !c.expectations.SatisfiedExpectations(key) {
		// expectations have not been met, come back later
		cLog.Debugln("cluster has not satisfied expectations yet")
		return nil
	}

	// Look through infra jobs that belong to this cluster.
	// If the job's generation corresponds to the cluster's current generation
	// sync the cluster status with the job's latest status.
	// If the job is active but does not correspond to the cluster's current generation
	// add it to the list of jobs to be terminated.
	invalidActiveJobs := []*v1batch.Job{}
	var currentInfraJob *v1batch.Job
	jobs, err := c.jobsLister.Jobs(cluster.Namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if isInfraJob(job) && isController(cluster, job) {
			switch {
			case jobClusterGeneration(job) == cluster.Generation:
				currentInfraJob = job
			case isActive(job):
				cLog.WithField("job", jobKey(job)).
					Debugln("found active job that does not correspond to cluster's current generation")
				invalidActiveJobs = append(invalidActiveJobs, job)
				break
			}
		}
	}

	// Determine expectations for creates and deletes
	shouldProvision := currentInfraJob == nil && cluster.Status.ProvisioningJobGeneration != cluster.Generation
	var expectCreate int
	if shouldProvision {
		expectCreate = 1
	}
	keysToDelete := []string{}
	for _, job := range invalidActiveJobs {
		keysToDelete = append(keysToDelete, jobKey(job))
	}
	c.expectations.SetExpectations(key, expectCreate, keysToDelete)

	// Delete invalid jobs asynchronously
	if len(invalidActiveJobs) > 0 {
		go c.deleteJobs(key, invalidActiveJobs)
	}

	// If a current job was found, sync with it
	if currentInfraJob != nil {
		cLog.Debugln("provisioning job exists, will sync with job")
		return c.syncClusterStatusWithJob(cluster, currentInfraJob)
	}

	// If no current job was found, the cluster is not yet provisioned,
	// and the status says there should be a job, the
	// job may have been deleted. Update status accordingly
	if currentInfraJob == nil && isClusterProvisioning(cluster) && cluster.Status.ProvisioningJobGeneration == cluster.Generation {
		return c.setJobNotFoundStatus(cluster)
	}

	if shouldProvision {
		return c.provisionCluster(cluster)
	}
	return nil
}

func (c *InfraController) deleteJobs(clusterKey string, jobs []*v1batch.Job) {
	for _, job := range jobs {
		err := c.kubeClient.BatchV1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		if err != nil {
			c.logger.WithField("job", jobKey(job)).Warningf("error deleting job: %v", err)
			utilruntime.HandleError(err)
			c.expectations.DeletionObserved(clusterKey, jobKey(job))
		}
	}
}

func isClusterProvisioning(cluster *clusteroperator.Cluster) bool {
	provisioning := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioning)
	return provisioning != nil && provisioning.Status == kapi.ConditionTrue
}

func jobKey(job *v1batch.Job) string {
	return fmt.Sprintf("%s/%s", job.Namespace, job.Name)
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
	if provisioning := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioning); provisioning != nil {
		provisioning.Status = kapi.ConditionFalse
		provisioning.Reason = reason
		provisioning.Message = message
		provisioning.LastTransitionTime = now
		provisioning.LastProbeTime = now
	}
	provisioningFailed := clusterCondition(cluster, clusteroperator.ClusterHardwareProvisioningFailed)
	if provisioningFailed != nil {
		provisioningFailed.Status = kapi.ConditionTrue
		provisioningFailed.Reason = reason
		provisioningFailed.Message = message
		provisioningFailed.LastTransitionTime = now
		provisioningFailed.LastProbeTime = now
	} else {
		cluster.Status.Conditions = append(cluster.Status.Conditions, clusteroperator.ClusterCondition{
			Type:               clusteroperator.ClusterHardwareProvisioningFailed,
			Status:             kapi.ConditionTrue,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: now,
			LastProbeTime:      now,
		})
	}
	return c.updateClusterStatus(original, cluster)
}

func (c *InfraController) provisionCluster(cluster *clusteroperator.Cluster) error {
	clusterLogger := c.logger.WithField("cluster", fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name))
	clusterLogger.Infoln("provisioning cluster infrastructure")
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for Cluster %s/%s: %v", cluster.Namespace, cluster.Name, err))
		return nil
	}
	varsGenerator := ansible.NewVarsGenerator(cluster)
	vars, err := varsGenerator.GenerateVars()
	if err != nil {
		return err
	}

	job, cfgMap := c.ansibleGenerator.GeneratePlaybookJob(cluster, jobPrefix, infraPlaybook, provisionInventoryTemplate, vars)

	_, err = c.kubeClient.CoreV1().ConfigMaps(cluster.Namespace).Create(cfgMap)
	if err != nil {
		return err
	}

	_, err = c.kubeClient.BatchV1().Jobs(cluster.Namespace).Create(job)
	if err != nil {
		// If an error occurred creating the job, remove expectation
		c.expectations.CreationObserved(clusterKey)
		return err
	}

	return nil
}
