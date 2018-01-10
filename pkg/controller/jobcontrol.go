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

package controller

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage/names"
	kubeclientset "k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

type JobControl interface {
	// ControlJobs handles running/deleting jobs for the owner.
	//
	// ownerKey: key to identify the owner (namespace/name)
	// owner: owner owning the jobs
	// jobFactory: function to call to build a new job if one does not already
	//   exist. if nil, then a new job is not needed.
	// logger: logger to which to log
	//
	// Return:
	//  (*kbatch.Job) current job
	//  (bool) true if the current job was newly created
	//  (error) error that preventing handling the jobs
	ControlJobs(ownerKey string, owner metav1.Object, jobFactory JobFactory, logger log.FieldLogger) (*kbatch.Job, bool, error)

	// ObserveOwnerDeletion observes that the owner with the specified key was
	// deleted.
	ObserveOwnerDeletion(ownerKey string)

	// ObserveJobCreation observes that a job was created for the owner with
	// the specified key.
	ObserveJobCreation(ownerKey string, job *kbatch.Job)

	// ObserveJobDeletion observes that a job created was deleted for the
	// owner with the specified key.
	ObserveJobDeletion(ownerKey string, job *kbatch.Job)

	// IsControlledJob tests that the job is controlled by this control.
	IsControlledJob(job *kbatch.Job) bool
}

type JobFactory interface {
	BuildJob(name string) (*kbatch.Job, *kapi.ConfigMap, error)
}

type jobControl struct {
	jobPrefix  string
	ownerKind  schema.GroupVersionKind
	kubeClient kubeclientset.Interface
	jobsLister batchlisters.JobLister
	// A TTLCache of job creations/deletions we're expecting to see
	expectations *UIDTrackingControllerExpectations
}

// NewJobControl creates a new JobControl.
func NewJobControl(jobPrefix string, ownerKind schema.GroupVersionKind, kubeClient kubeclientset.Interface, jobsLister batchlisters.JobLister) JobControl {
	return &jobControl{
		jobPrefix:    jobPrefix,
		ownerKind:    ownerKind,
		kubeClient:   kubeClient,
		jobsLister:   jobsLister,
		expectations: NewUIDTrackingControllerExpectations(NewControllerExpectations()),
	}
}

func (c *jobControl) ControlJobs(ownerKey string, owner metav1.Object, jobFactory JobFactory, logger log.FieldLogger) (*kbatch.Job, bool, error) {
	if !c.expectations.SatisfiedExpectations(ownerKey) {
		// expectations have not been met, come back later
		logger.Debugln("expectations have not been satisfied yet")
		return nil, false, nil
	}

	// Look through jobs that belong to the owner and that match the type
	// of job created by this control.
	// If the job's generation corresponds to the owner's current
	// generation, sync the owner status with the job's latest status.
	// If the job does not correspond to the owner's current generation,
	// delete the job.
	oldJobs := []*kbatch.Job{}
	var currentJob *kbatch.Job
	jobs, err := c.jobsLister.Jobs(owner.GetNamespace()).List(labels.Everything())
	if err != nil {
		return nil, false, err
	}
	for _, job := range jobs {
		if !metav1.IsControlledBy(job, owner) {
			continue
		}
		if !c.IsControlledJob(job) {
			continue
		}
		if jobOwnerGeneration(job) == owner.GetGeneration() {
			currentJob = job
		} else {
			logger.WithField("job", jobKey(job)).
				Debugln("found job that does not correspond to owner's current generation")
			oldJobs = append(oldJobs, job)
		}
	}

	if len(oldJobs) > 0 {
		return nil, false, c.deleteOldJobs(ownerKey, oldJobs, logger)
	}

	if currentJob != nil {
		return currentJob, false, nil
	}

	if jobFactory != nil {
		job, err := c.createJob(ownerKey, owner, jobFactory, logger)
		return job, job != nil, err
	}

	return nil, false, nil
}

func (c *jobControl) ObserveOwnerDeletion(ownerKey string) {
	c.expectations.DeleteExpectations(ownerKey)
}

func (c *jobControl) ObserveJobCreation(ownerKey string, job *kbatch.Job) {
	c.expectations.CreationObserved(ownerKey)
}

func (c *jobControl) ObserveJobDeletion(ownerKey string, job *kbatch.Job) {
	c.expectations.DeletionObserved(ownerKey, jobKey(job))
}

func (c *jobControl) IsControlledJob(job *kbatch.Job) bool {
	return strings.HasPrefix(job.Name, c.jobPrefix)
}

func (c *jobControl) createJob(ownerKey string, owner metav1.Object, jobFactory JobFactory, logger log.FieldLogger) (*kbatch.Job, error) {
	logger.Infof("Creating new %q job", c.jobPrefix)

	name := names.SimpleNameGenerator.GenerateName(fmt.Sprintf("%s%s-", c.jobPrefix, owner.GetName()))

	job, configMap, err := jobFactory.BuildJob(name)
	if err != nil {
		return nil, err
	}

	if job == nil {
		return nil, nil
	}

	ownerRef := metav1.NewControllerRef(owner, c.ownerKind)

	job.OwnerReferences = append(job.OwnerReferences, *ownerRef)
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[clusteroperator.OwnerGenerationAnnotation] = fmt.Sprintf("%d", owner.GetGeneration())

	cleanUpConfigMap := true
	if configMap != nil {
		configMap.OwnerReferences = append(configMap.OwnerReferences, *ownerRef)

		configMap, err = c.kubeClient.CoreV1().ConfigMaps(owner.GetNamespace()).Create(configMap)
		if err != nil {
			return nil, err
		}
		defer func() {
			if configMap != nil && cleanUpConfigMap {
				if err := c.kubeClient.CoreV1().ConfigMaps(configMap.Namespace).Delete(configMap.Name, &metav1.DeleteOptions{}); err != nil {
					logger.Warnf("Could not delete config map %v/%v", configMap.Namespace, configMap.Name)
				}
			}
		}()
	}

	if err := c.expectations.ExpectCreations(ownerKey, 1); err != nil {
		return nil, err
	}

	newJob, err := c.kubeClient.BatchV1().Jobs(owner.GetNamespace()).Create(job)
	if err != nil {
		// If an error occurred creating the job, remove expectation
		c.expectations.CreationObserved(ownerKey)
		return nil, err
	}

	cleanUpConfigMap = false

	return newJob, nil
}

func (c *jobControl) deleteOldJobs(ownerKey string, oldJobs []*kbatch.Job, logger log.FieldLogger) error {
	keysToDelete := []string{}
	for _, job := range oldJobs {
		keysToDelete = append(keysToDelete, jobKey(job))
	}
	c.expectations.ExpectDeletions(ownerKey, keysToDelete)

	logger.Infof("Deleting old jobs: %v", keysToDelete)

	var errCh chan error

	var wg sync.WaitGroup
	wg.Add(len(oldJobs))
	for i, ng := range oldJobs {
		go func(ix int, job *kbatch.Job) {
			defer wg.Done()
			if err := c.kubeClient.BatchV1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{}); err != nil {
				// Decrement the expected number of deletes because the informer won't observe this deletion
				jobKey := keysToDelete[ix]
				logger.Infof("Failed to delete %v, decrementing expectations", jobKey)
				c.expectations.DeletionObserved(ownerKey, jobKey)
				errCh <- err
			}
		}(i, ng)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		// all errors have been reported before and they're likely to be the same, so we'll only return the first one we hit.
		if err != nil {
			return err
		}
	default:
	}
	return nil
}

func jobOwnerGeneration(job *kbatch.Job) int64 {
	if job.Annotations == nil {
		return 0
	}
	generationStr, ok := job.Annotations[clusteroperator.OwnerGenerationAnnotation]
	if !ok {
		return 0
	}
	generation, err := strconv.ParseInt(generationStr, 10, 64)
	if err != nil {
		return 0
	}
	return generation
}

func jobKey(job *kbatch.Job) string {
	return fmt.Sprintf("%s/%s", job.Namespace, job.Name)
}
