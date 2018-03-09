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
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift/cluster-operator/test"
)

const (
	testNamespace = "test-namespace"
	testName      = "test-name"
	testKey       = "test-namespace/test-name"
	testJobName   = "test-job-abc"
	testFinalizer = "openshift/cluster-operator-test-job-1"
)

var (
	testGroupResource = schema.GroupResource{
		Group:    "test-group",
		Resource: "test-resource",
	}
)

// TestJobSyncForRemovedOwner tests jobSync.Sync when the owner has been
// removed from storage.
func TestJobSyncForRemovedOwner(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(nil, errors.NewNotFound(testGroupResource, testName))
	mockJobControl.EXPECT().ObserveOwnerDeletion(testKey)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.NoError(t, err, "unexpected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithErrorGettingOwner tests jobSync.Sync when there is an error
// getting the owner.
func TestJobSyncWithErrorGettingOwner(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(nil, fmt.Errorf("error getting owner"))

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.Error(t, err, "expected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncForDeletedOwner tests jobSync.Sync when the owner has been
// marked for deletion.
func TestJobSyncForDeletedOwner(t *testing.T) {
	cases := []struct {
		name          string
		undoOnDelete  bool
		hasFinalizer  bool
		expectControl bool
	}{
		{
			name:          "no undo",
			undoOnDelete:  false,
			expectControl: false,
		},
		{
			name:          "no finalizer",
			undoOnDelete:  true,
			hasFinalizer:  false,
			expectControl: false,
		},
		{
			name:          "with finalizer",
			undoOnDelete:  true,
			hasFinalizer:  true,
			expectControl: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			logger, loggerHook := test.Logger()

			mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
			mockJobControl := NewMockJobControl(mockCtrl)
			mockJobFactory := NewMockJobFactory(mockCtrl)

			now := metav1.Now()
			owner := &metav1.ObjectMeta{
				DeletionTimestamp: &now,
			}
			if tc.hasFinalizer {
				owner.Finalizers = []string{testFinalizer}
			}

			mockJobSyncStrategy.EXPECT().GetOwner(testKey).
				Return(owner, nil)

			if tc.undoOnDelete {
				mockJobControl.EXPECT().GetJobPrefix().
					Return(testJobPrefix)
			}

			if tc.expectControl {
				mockJobSyncStrategy.EXPECT().GetJobFactory(owner, true).
					Return(mockJobFactory, nil)
				mockJobControl.EXPECT().ControlJobs(testKey, owner, true, mockJobFactory).
					Return(JobControlNoWork, nil, nil)
			}

			jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, tc.undoOnDelete, logger)
			err := jobSync.Sync(testKey)

			assert.NoError(t, err, "unexpected error from Sync")

			assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
		})
	}
}

// TestJobSyncWithErrorGettingJobFactory tests jobSync.Sync when there is an
// error getting the job factory.
func TestJobSyncWithErrorGettingJobFactory(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)

	owner := &metav1.ObjectMeta{}

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(true).
		AnyTimes()
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(nil, fmt.Errorf("error getting job factory"))

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.Error(t, err, "expected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithErrorControllingJobs tests jobSync.Sync when there is an
// error from JobControl.ControlJobs.
func TestJobSyncWithErrorControllingJobs(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	needsProcessing := true

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlResult(""), nil, fmt.Errorf("error controlling jobs"))

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.Error(t, err, "expected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithPendingExpectationsResult tests jobSync.Sync when
// ControlJobs returns that there are pending expectations.
func TestJobSyncWithPendingExpectationsResult(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	needsProcessing := true

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlPendingExpectations, nil, nil)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.NoError(t, err, "unexpected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithNoWorkResult tests jobSync.Sync when ControlJobs returns
// that there is no work to do.
func TestJobSyncWithNoWorkResult(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	needsProcessing := true

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlNoWork, nil, nil)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.NoError(t, err, "unexpected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncForCompletedJob tests jobSync.Sync when ControlJobs returns that
// there is a job working and the job is completed.
func TestJobSyncForCompletedJob(t *testing.T) {
	cases := []struct {
		name     string
		deleting bool
	}{
		{
			name:     "processing",
			deleting: false,
		},
		{
			name:     "undoing",
			deleting: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			logger, loggerHook := test.Logger()

			mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
			mockJobControl := NewMockJobControl(mockCtrl)
			mockJobFactory := NewMockJobFactory(mockCtrl)

			owner := &metav1.ObjectMeta{}
			ownerCopy := &metav1.ObjectMeta{}
			if tc.deleting {
				now := metav1.Now()
				owner.DeletionTimestamp = &now
				owner.Finalizers = []string{testFinalizer}
				ownerCopy.DeletionTimestamp = &now
			}

			jobTransitionTime := metav1.Date(2018, time.February, 1, 2, 3, 4, 5, time.UTC)
			job := &kbatch.Job{
				Status: kbatch.JobStatus{
					Conditions: []kbatch.JobCondition{
						{
							Type:               kbatch.JobComplete,
							Status:             kapi.ConditionTrue,
							Reason:             "Completed",
							Message:            "Done",
							LastTransitionTime: jobTransitionTime,
							LastProbeTime:      jobTransitionTime,
						},
					},
				},
			}

			mockJobSyncStrategy.EXPECT().GetOwner(testKey).
				Return(owner, nil)
			if !tc.deleting {
				mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
					Return(true)
			} else {
				mockJobControl.EXPECT().GetJobPrefix().
					AnyTimes().
					Return(testJobPrefix)
			}
			mockJobSyncStrategy.EXPECT().GetJobFactory(owner, tc.deleting).
				Return(mockJobFactory, nil)
			mockJobControl.EXPECT().ControlJobs(testKey, owner, true, mockJobFactory).
				Return(JobControlJobSucceeded, job, nil)

			// Update owner status to reflect completed job
			mockJobSyncStrategy.EXPECT().DeepCopyOwner(owner).
				Return(ownerCopy)
			if tc.deleting {
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncUndoing, kapi.ConditionFalse, ReasonJobCompleted, gomock.Any(), gomock.Any())
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessed, kapi.ConditionFalse, ReasonJobCompleted, gomock.Any(), gomock.Any())
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncUndoFailed, kapi.ConditionFalse, ReasonJobCompleted, gomock.Any(), gomock.Any())
			} else {
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessing, kapi.ConditionFalse, ReasonJobCompleted, gomock.Any(), gomock.Any())
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessed, kapi.ConditionTrue, ReasonJobCompleted, gomock.Any(), gomock.Any())
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessingFailed, kapi.ConditionFalse, ReasonJobCompleted, gomock.Any(), gomock.Any())
			}
			if !tc.deleting {
				mockJobSyncStrategy.EXPECT().OnJobCompletion(ownerCopy, true)
			}
			mockJobSyncStrategy.EXPECT().UpdateOwnerStatus(owner, ownerCopy)

			jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, true, logger)
			err := jobSync.Sync(testKey)

			assert.NoError(t, err, "unexpected error from Sync")

			assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")

		})
	}
}

// TestJobSyncForFailedJob tests jobSync.Sync when ControlJobs returns that
// there is a job working and the job has failed.
func TestJobSyncForFailedJob(t *testing.T) {
	cases := []struct {
		name     string
		deleting bool
	}{
		{
			name:     "processing",
			deleting: false,
		},
		{
			name:     "undoing",
			deleting: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			logger, loggerHook := test.Logger()

			mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
			mockJobControl := NewMockJobControl(mockCtrl)
			mockJobFactory := NewMockJobFactory(mockCtrl)

			owner := &metav1.ObjectMeta{}
			ownerCopy := &metav1.ObjectMeta{}
			if tc.deleting {
				now := metav1.Now()
				owner.DeletionTimestamp = &now
				owner.Finalizers = []string{testFinalizer}
				ownerCopy.DeletionTimestamp = &now
			}

			jobTransitionTime := metav1.Date(2018, time.February, 1, 2, 3, 4, 5, time.UTC)
			job := &kbatch.Job{
				Status: kbatch.JobStatus{
					Conditions: []kbatch.JobCondition{
						{
							Type:               kbatch.JobFailed,
							Status:             kapi.ConditionTrue,
							Reason:             "Failed",
							Message:            "Done",
							LastTransitionTime: jobTransitionTime,
							LastProbeTime:      jobTransitionTime,
						},
					},
				},
			}

			mockJobSyncStrategy.EXPECT().GetOwner(testKey).
				Return(owner, nil)
			if !tc.deleting {
				mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
					Return(true)
			} else {
				mockJobControl.EXPECT().GetJobPrefix().
					AnyTimes().
					Return(testJobPrefix)
			}
			mockJobSyncStrategy.EXPECT().GetJobFactory(owner, tc.deleting).
				Return(mockJobFactory, nil)
			mockJobControl.EXPECT().ControlJobs(testKey, owner, true, mockJobFactory).
				Return(JobControlJobFailed, job, nil)

			// Update owner status to reflect failed job
			mockJobSyncStrategy.EXPECT().DeepCopyOwner(owner).
				Return(ownerCopy)
			if tc.deleting {
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncUndoing, kapi.ConditionFalse, ReasonJobFailed, gomock.Any(), gomock.Any())
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncUndoFailed, kapi.ConditionTrue, ReasonJobFailed, gomock.Any(), gomock.Any())
			} else {
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessing, kapi.ConditionFalse, ReasonJobFailed, gomock.Any(), gomock.Any())
				mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessingFailed, kapi.ConditionTrue, ReasonJobFailed, gomock.Any(), gomock.Any())
			}
			if !tc.deleting {
				mockJobSyncStrategy.EXPECT().OnJobCompletion(ownerCopy, false)
			}
			mockJobSyncStrategy.EXPECT().UpdateOwnerStatus(owner, ownerCopy)

			jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, true, logger)
			err := jobSync.Sync(testKey)

			assert.NoError(t, err, "unexpected error from Sync")

			assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
		})
	}
}

// TestJobSyncForInProgressJob tests jobSync.Sync when ControlJobs returns
// that there is a job working and the job is still in progress.
func TestJobSyncForInProgressJob(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	ownerCopy := &metav1.ObjectMeta{}
	needsProcessing := true

	job := &kbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "in-progress-job",
		},
	}

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlJobWorking, job, nil)

	// Update owner status to reflect in-progress job
	mockJobSyncStrategy.EXPECT().DeepCopyOwner(owner).
		Return(ownerCopy)
	mockJobSyncStrategy.EXPECT().SetOwnerJobSyncCondition(ownerCopy, JobSyncProcessing, kapi.ConditionTrue, ReasonJobRunning, gomock.Any(), gomock.Any())
	mockJobSyncStrategy.EXPECT().UpdateOwnerStatus(owner, ownerCopy)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.NoError(t, err, "unexpected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithJobWorkingResultButNoJobReturned tests jobSync.Sync when
// ControlJobs returns that there is a job working but did not return any job.
func TestJobSyncWithJobWorkingResultButNoJobReturned(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	needsProcessing := true

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlJobWorking, nil, nil)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.Error(t, err, "expected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithCreatingJobResult tests jobSync.Sync when
// ControlJobs returns that it is creating a job.
func TestJobSyncWithCreatingJobResult(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	needsProcessing := true

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlCreatingJob, nil, nil)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.NoError(t, err, "unexpected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobSyncWithUnkownJobsResult tests jobSync.Sync when ControlJobs returns
// an unknown JobControlResult.
func TestJobSyncWithUnkownJobsResult(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	logger, loggerHook := test.Logger()

	mockJobSyncStrategy := NewMockJobSyncStrategy(mockCtrl)
	mockJobControl := NewMockJobControl(mockCtrl)
	mockJobFactory := NewMockJobFactory(mockCtrl)

	owner := &metav1.ObjectMeta{}
	needsProcessing := true

	mockJobSyncStrategy.EXPECT().GetOwner(testKey).
		Return(owner, nil)
	mockJobSyncStrategy.EXPECT().DoesOwnerNeedProcessing(owner).
		Return(needsProcessing)
	mockJobSyncStrategy.EXPECT().GetJobFactory(owner, false).
		Return(mockJobFactory, nil)
	mockJobControl.EXPECT().ControlJobs(testKey, owner, needsProcessing, mockJobFactory).
		Return(JobControlResult("other-result"), nil, nil)

	jobSync := NewJobSync(mockJobControl, mockJobSyncStrategy, false, logger)
	err := jobSync.Sync(testKey)

	assert.Error(t, err, "expected error from Sync")

	assert.Empty(t, test.GetDireLogEntries(loggerHook), "unexpected dire log entries")
}
