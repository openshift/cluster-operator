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
	"time"

	v1batch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:generate mockgen -source=./jobsyncstrategy.go -destination=./mockjobsyncstrategy_generated_test.go -package=controller

// JobSyncStrategy provides a strategy to the job sync for details on how
// to sync for the controller.
type JobSyncStrategy interface {
	// GetOwner gets the owner object with the specified key.
	GetOwner(key string) (metav1.Object, error)

	// DoesOwnerNeedProcessing returns true if the owner is not up to date
	// and needs a processing job to bring the owner up to date.
	DoesOwnerNeedProcessing(owner metav1.Object) bool

	// GetJobFactory gets a factory for building a job to do the processing.
	GetJobFactory(owner metav1.Object, deleting bool) (JobFactory, error)

	// DeepCopyOwner returns a deep copy of the owner object.
	DeepCopyOwner(owner metav1.Object) metav1.Object

	// SetOwnerJobSyncCondition sets the specified condition for the specified
	// owner.
	SetOwnerJobSyncCondition(
		owner metav1.Object,
		conditionType JobSyncConditionType,
		status kapi.ConditionStatus,
		reason string,
		message string,
		updateConditionCheck UpdateConditionCheck,
	)

	// OnJobCompletion is called when the processing job for the owner
	// completes.
	OnJobCompletion(owner metav1.Object, job *v1batch.Job, succeeded bool)

	// UpdateOwnerStatus updates the status of the owner from the original
	// copy to the owner copy.
	UpdateOwnerStatus(original, owner metav1.Object) error

	// GetReprocessInterval returns the approximate interval at which we would like this job re-run
	// for on-going config management. Actual runtime will be randomized and potentially up to 2x the
	// value returned here. Controllers which do not want to support reprocessing can return nil.
	GetReprocessInterval() *time.Duration

	// GetLastJobSuccess returns the time of the last successful job. Used in conjunction with
	// GetReprocessInterval to determine if a new job needs to be launched. Controllers which
	// do not want to support reprocessing should return nil.
	GetLastJobSuccess(owner metav1.Object) *time.Time
}
