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
	"time"

	log "github.com/sirupsen/logrus"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobSync is used by a controller to sync an object that uses a job to do
// its processing.
type JobSync interface {
	// Sync syncs the object with the specified key.
	Sync(key string) error
}

// JobSyncConditionType is the type of condition that the job sync is
// adjusting on the owner object.
type JobSyncConditionType string

const (
	// JobSyncProcessing indicates that the processing job is in progress.
	JobSyncProcessing JobSyncConditionType = "Processing"
	// JobSyncProcessed indicates that the processing job has completed
	// successfully.
	JobSyncProcessed JobSyncConditionType = "Processed"
	// JobSyncProcessingFailed indicates that the processing job has failed.
	JobSyncProcessingFailed JobSyncConditionType = "ProcessingFailed"
)

const (
	// ReasonJobRunning is a condition reason used when a job is still
	// running.
	ReasonJobRunning = "JobRunning"
	// ReasonJobCompleted is a condition reason used when a job has been
	// completed successfully.
	ReasonJobCompleted = "JobCompleted"
	// ReasonJobFailed is a condition reason used when a job has failed.
	ReasonJobFailed = "JobFailed"
	// ReasonJobMissing is a condition reason used when a job that was
	// expected to exist does not exist.
	ReasonJobMissing = "JobMissing"
	// ReasonSpecChanged is a condition reason used when the spec of an
	// object changes, invalidating existing jobs.
	ReasonSpecChanged = "SpecChanged"
)

// JobSyncStrategy provides a strategy to the job sync for details on how
// to sync for the controller.
type JobSyncStrategy interface {
	// EnqueueOwner enqueues the specified owner in the controller.
	EnqueueOwner(owner metav1.Object)

	// GetOwner gets the owner object with the specified key.
	GetOwner(key string) (metav1.Object, error)

	// DoesOwnerNeedProcessing returns true if the owner is not up to date
	// and needs a processing job to bring the owner up to date.
	DoesOwnerNeedProcessing(owner metav1.Object) bool

	// GetJobFactory gets a factory for building a job to do the processing.
	GetJobFactory(owner metav1.Object) (JobFactory, error)

	// GetOwnerJobSyncConditionStatus gets the status of the specified
	// condition for the specified owner.
	GetOwnerJobSyncConditionStatus(owner metav1.Object, conditionType JobSyncConditionType) bool

	// DeepCopyOwner returns a deep copy of the owner object.
	DeepCopyOwner(owner metav1.Object) metav1.Object

	// SetOwnerJobSyncCondition sets the specified condition for the specified
	// owner.
	// The updateConditionCheck functions are used to determine whether
	// the condition should be updated if there are changes to the reason or
	// the message of the condition.
	SetOwnerJobSyncCondition(
		owner metav1.Object,
		conditionType JobSyncConditionType,
		status kapi.ConditionStatus,
		reason string,
		message string,
		updateConditionCheck ...UpdateConditionCheck,
	)

	// OnJobCompletion is called when the processing job for the owner
	// completes successfully.
	OnJobCompletion(owner metav1.Object)

	// OnJobFailure is called when the processing job for the owner fails.
	OnJobFailure(owner metav1.Object)

	// UpdateOwnerStatus updates the status of the owner from the original
	// copy to the owner copy.
	UpdateOwnerStatus(original, owner metav1.Object) error

	// ProcessDeletedOwner processes an owner that has been marked for
	// deletion.
	ProcessDeletedOwner(owner metav1.Object) error
}

type jobSync struct {
	jobControl JobControl
	strategy   JobSyncStrategy
	logger     log.FieldLogger
}

// NewJobSync creates a new JobSync.
func NewJobSync(jobControl JobControl, strategy JobSyncStrategy, logger log.FieldLogger) JobSync {
	return &jobSync{
		jobControl: jobControl,
		strategy:   strategy,
		logger:     logger,
	}
}

func (s *jobSync) Sync(key string) error {
	logger := log.FieldLogger(s.logger.WithField("key", key))
	startTime := time.Now()
	logger.Debugln("Started syncing")
	defer logger.WithField("duration", time.Since(startTime)).Debugln("Finished syncing")

	owner, err := s.strategy.GetOwner(key)
	if errors.IsNotFound(err) {
		logger.Debugln("owner has been deleted")
		s.jobControl.ObserveOwnerDeletion(key)
		return nil
	}
	if err != nil {
		return err
	}

	logger = loggerForOwner(s.logger, owner)

	// Are we dealing with an owner marked for deletion
	if owner.GetDeletionTimestamp() != nil {
		logger.Debugf("DeletionTimestamp set")
		return s.strategy.ProcessDeletedOwner(owner)
	}

	needsProcessing := s.strategy.DoesOwnerNeedProcessing(owner)

	jobFactory, err := s.strategy.GetJobFactory(owner)
	if err != nil {
		return err
	}

	job, isJobNew, err := s.jobControl.ControlJobs(key, owner, needsProcessing, jobFactory)
	if err != nil {
		return err
	}

	switch {
	// Owner is update to date
	case !needsProcessing:
		return nil
	// New job has not been created, so an old job must exist. Set the owner
	// to not processing as the old job is deleted.
	case job == nil:
		return s.setOwnerToNotProcessing(owner)
	// Job was not newly created, so sync owner status with job.
	case !isJobNew:
		logger.Debugln("job exists, will sync with job")
		return s.syncOwnerStatusWithJob(owner, job)
	// Owner should have a job but it was not found.
	case s.strategy.GetOwnerJobSyncConditionStatus(owner, JobSyncProcessing):
		return s.setJobNotFoundStatus(owner)
	// New job created
	default:
		return nil
	}
}

// setOwnerToNotProcessing updates the processing condition
// for the owner to reflect that an in-progress job is no longer processing
// due to a change in the spec of the owner.
func (s *jobSync) setOwnerToNotProcessing(original metav1.Object) error {
	owner := s.strategy.DeepCopyOwner(original)
	s.strategy.SetOwnerJobSyncCondition(
		owner,
		JobSyncProcessing,
		kapi.ConditionFalse,
		ReasonSpecChanged,
		"Spec changed. New job needed",
	)
	return s.strategy.UpdateOwnerStatus(original, owner)
}

// syncOwnerStatusWithJob update the status of the owner to
// reflect the current status of the job that is processing the owner.
// If the job completed successfully, the owner will be marked as
// processed.
// If the job completed with a failure, the owner will be marked as
// not processed.
// If the job is still in progress, the owner will be marked as
// processing.
func (s *jobSync) syncOwnerStatusWithJob(original metav1.Object, job *v1batch.Job) error {
	owner := s.strategy.DeepCopyOwner(original)

	jobCompleted := findJobCondition(job, v1batch.JobComplete)
	jobFailed := findJobCondition(job, v1batch.JobFailed)

	switch {
	// Job completed successfully
	case jobCompleted != nil && jobCompleted.Status == kapi.ConditionTrue:
		reason := ReasonJobCompleted
		message := fmt.Sprintf("Job %s/%s completed at %v", job.Namespace, job.Name, jobCompleted.LastTransitionTime)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessing, kapi.ConditionFalse, reason, message)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessed, kapi.ConditionTrue, reason, message)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessingFailed, kapi.ConditionFalse, reason, message)
		s.strategy.OnJobCompletion(owner)
	// Job failed
	case jobFailed != nil && jobFailed.Status == kapi.ConditionTrue:
		reason := ReasonJobFailed
		message := fmt.Sprintf("Job %s/%s failed at %v, reason: %s", job.Namespace, job.Name, jobFailed.LastTransitionTime, jobFailed.Reason)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessing, kapi.ConditionFalse, reason, message)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessingFailed, kapi.ConditionTrue, reason, message)
		s.strategy.OnJobFailure(owner)
	// Job in progress
	default:
		s.strategy.SetOwnerJobSyncCondition(
			owner,
			JobSyncProcessing,
			kapi.ConditionTrue,
			ReasonJobRunning,
			fmt.Sprintf("Job %s/%s is running since %v. Pod completions: %d, failures: %d", job.Namespace, job.Name, job.Status.StartTime, job.Status.Succeeded, job.Status.Failed),
			func(oldReason, oldMessage, newReason, newMessage string) bool {
				return oldReason != newReason || newMessage != oldMessage
			},
		)
	}

	return s.strategy.UpdateOwnerStatus(original, owner)
}

func (s *jobSync) setJobNotFoundStatus(original metav1.Object) error {
	owner := s.strategy.DeepCopyOwner(original)
	reason := ReasonJobMissing
	message := "Job not found."
	s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessing, kapi.ConditionFalse, reason, message)
	s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessingFailed, kapi.ConditionTrue, reason, message)
	return s.strategy.UpdateOwnerStatus(original, owner)
}

// findJobCondition finds in the job the condition that has the
// specified condition type. If none exists, then returns nil.
func findJobCondition(job *v1batch.Job, conditionType v1batch.JobConditionType) *v1batch.JobCondition {
	for i, condition := range job.Status.Conditions {
		if condition.Type == conditionType {
			return &job.Status.Conditions[i]
		}
	}
	return nil
}
