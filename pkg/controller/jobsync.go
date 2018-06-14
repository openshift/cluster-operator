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
	"k8s.io/apimachinery/pkg/util/sets"
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
	// JobSyncUndoing indicates that the undoing job is in progress.
	JobSyncUndoing JobSyncConditionType = "Undoing"
	// JobSyncUndoFailed indicates that the undoing job has failed.
	JobSyncUndoFailed JobSyncConditionType = "UndoFailed"
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

type jobSync struct {
	jobControl   JobControl
	strategy     JobSyncStrategy
	undoOnDelete bool
	logger       log.FieldLogger
}

// NewJobSync creates a new JobSync.
func NewJobSync(jobControl JobControl, strategy JobSyncStrategy, undoOnDelete bool, logger log.FieldLogger) JobSync {
	return &jobSync{
		jobControl:   jobControl,
		strategy:     strategy,
		undoOnDelete: undoOnDelete,
		logger:       logger,
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

	deleting := false

	// Are we dealing with an owner marked for deletion
	if owner.GetDeletionTimestamp() != nil {
		if !s.undoOnDelete {
			return nil
		}
		if !s.hasFinalizer(owner) {
			return nil
		}
		// If the strategy implements CheckBeforeUndo and it returns false for CanUndo, then we should
		// return and try again later.
		if checkBeforeUndo, hasDeprovisionCheck := s.strategy.(CheckBeforeUndo); hasDeprovisionCheck {
			if !checkBeforeUndo.CanUndo(owner) {
				return nil
			}
		}
		logger.Debugf("Undoing job processing on delete")
		deleting = true
	}

	needsProcessing := deleting || s.strategy.DoesOwnerNeedProcessing(owner)

	jobFactory, err := s.strategy.GetJobFactory(owner, deleting)
	if err != nil {
		return err
	}

	var (
		reprocessInterval *time.Duration
		lastJobSuccess    *time.Time
	)
	if reprocessStrategy, shouldReprocess := s.strategy.(JobSyncReprocessStrategy); shouldReprocess {
		ri := reprocessStrategy.GetReprocessInterval()
		reprocessInterval = &ri
		lastJobSuccess = reprocessStrategy.GetLastJobSuccess(owner)
	}

	extraJobIdentifier := ""
	if deleting {
		extraJobIdentifier = "undo"
	}

	jobControlResult, job, err := s.jobControl.ControlJobs(key, owner, extraJobIdentifier,
		needsProcessing, reprocessInterval, lastJobSuccess, jobFactory)
	if err != nil {
		return err
	}

	logger.WithFields(log.Fields{
		"jobControlResult": jobControlResult,
	}).Debugln("ControlJobs")
	if job != nil {
		logger.WithFields(log.Fields{
			"job": job.Name,
		}).Debugln("got job")
	}

	switch jobControlResult {
	case JobControlJobSucceeded:
		return s.setOwnerStatusForSuccessfulJob(owner, job, deleting)
	case JobControlJobFailed:
		return s.setOwnerStatusForFailedJob(owner, job, deleting)
	case JobControlJobWorking:
		return s.setOwnerStatusForInProgressJob(owner, job, deleting)
	case JobControlDeletingJobs:
		return s.setOwnerStatusForOutdatedJob(owner)
	case JobControlCreatingJob:
		logger.Debugf("creating job")
		if s.undoOnDelete {
			logger.Debugf("adding finalizer")
			return s.addFinalizer(owner)
		}
		return nil
	case JobControlPendingExpectations, JobControlNoWork:
		return nil
	default:
		return fmt.Errorf("unknown job control result: %v", jobControlResult)
	}
}

// setOwnerStatusForOutdatedJob updates the processing condition
// for the owner to reflect that an in-progress job is no longer processing
// due to a change in the spec of the owner.
func (s *jobSync) setOwnerStatusForOutdatedJob(original metav1.Object) error {
	owner := s.strategy.DeepCopyOwner(original)
	s.strategy.SetOwnerJobSyncCondition(
		owner,
		JobSyncProcessing,
		kapi.ConditionFalse,
		ReasonSpecChanged,
		"Spec changed. New job needed",
		UpdateConditionNever,
	)
	return s.strategy.UpdateOwnerStatus(original, owner)
}

func (s *jobSync) setOwnerStatusForSuccessfulJob(original metav1.Object, job *v1batch.Job, deleting bool) error {
	owner := s.strategy.DeepCopyOwner(original)
	reason := ReasonJobCompleted
	message := "Job completed successful"
	if deleting {
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncUndoing, kapi.ConditionFalse, reason, message, UpdateConditionNever)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessed, kapi.ConditionFalse, reason, message, UpdateConditionNever)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncUndoFailed, kapi.ConditionFalse, reason, message, UpdateConditionNever)
	} else {
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessing, kapi.ConditionFalse, reason, message, UpdateConditionNever)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessed, kapi.ConditionTrue, reason, message, UpdateConditionNever)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessingFailed, kapi.ConditionFalse, reason, message, UpdateConditionNever)
	}
	if deleting {
		finalizerName := s.getFinalizerName()
		finalizers := sets.NewString(owner.GetFinalizers()...)
		finalizers.Delete(finalizerName)
		owner.SetFinalizers(finalizers.List())
	} else {
		if s.undoOnDelete {
			s.addFinalizerWithoutSaving(owner)
		}
		s.strategy.OnJobCompletion(owner, job, true)
	}
	return s.strategy.UpdateOwnerStatus(original, owner)
}

func (s *jobSync) setOwnerStatusForFailedJob(original metav1.Object, job *v1batch.Job, deleting bool) error {
	owner := s.strategy.DeepCopyOwner(original)
	reason := ReasonJobFailed
	message := "Job failed"
	if deleting {
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncUndoing, kapi.ConditionFalse, reason, message, UpdateConditionNever)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncUndoFailed, kapi.ConditionTrue, reason, message, UpdateConditionNever)
	} else {
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessing, kapi.ConditionFalse, reason, message, UpdateConditionNever)
		s.strategy.SetOwnerJobSyncCondition(owner, JobSyncProcessingFailed, kapi.ConditionTrue, reason, message, UpdateConditionNever)
	}
	if deleting {
		finalizerName := s.getFinalizerName()
		finalizers := sets.NewString(owner.GetFinalizers()...)
		finalizers.Delete(finalizerName)
		owner.SetFinalizers(finalizers.List())
	} else {
		if s.undoOnDelete {
			s.addFinalizerWithoutSaving(owner)
		}
		s.strategy.OnJobCompletion(owner, job, false)
	}
	return s.strategy.UpdateOwnerStatus(original, owner)
}

func (s *jobSync) setOwnerStatusForInProgressJob(original metav1.Object, job *v1batch.Job, deleting bool) error {
	if job == nil {
		return fmt.Errorf("job control result was that a job was working, but no job was returned")
	}
	owner := s.strategy.DeepCopyOwner(original)
	if !deleting && s.undoOnDelete {
		s.addFinalizerWithoutSaving(owner)
	}
	workingCondtion := JobSyncProcessing
	if deleting {
		workingCondtion = JobSyncUndoing
	}
	reason := ReasonJobRunning
	message := "Job running"
	s.strategy.SetOwnerJobSyncCondition(
		owner,
		workingCondtion,
		kapi.ConditionTrue,
		reason,
		message,
		UpdateConditionIfReasonOrMessageChange,
	)
	return s.strategy.UpdateOwnerStatus(original, owner)
}

func (s *jobSync) hasFinalizer(owner metav1.Object) bool {
	finalizer := s.getFinalizerName()
	return HasFinalizer(owner, finalizer)
}

func (s *jobSync) addFinalizer(original metav1.Object) error {
	if s.hasFinalizer(original) {
		return nil
	}
	owner := s.strategy.DeepCopyOwner(original)
	s.addFinalizerWithoutSaving(owner)
	return s.strategy.UpdateOwnerStatus(original, owner)
}

func (s *jobSync) addFinalizerWithoutSaving(owner metav1.Object) {
	if s.hasFinalizer(owner) {
		return
	}
	finalizers := sets.NewString(owner.GetFinalizers()...)
	finalizers.Insert(s.getFinalizerName())
	owner.SetFinalizers(finalizers.List())
}

func (s *jobSync) getFinalizerName() string {
	return fmt.Sprintf("openshift/cluster-operator-%s", s.jobControl.GetJobPrefix())
}
