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
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// BaseControllerRefManager is the base for an controller/owner reference manager
type BaseControllerRefManager struct {
	Controller metav1.Object
	Selector   labels.Selector

	canAdoptErr  error
	canAdoptOnce sync.Once
	CanAdoptFunc func() error
}

// CanAdopt calls the canAdoptFunc exactly once and stores its
// result in canAdoptErr. Subsequent calls to this function will
// simply return the stored canAdoptErr value.
func (m *BaseControllerRefManager) CanAdopt() error {
	m.canAdoptOnce.Do(func() {
		if m.CanAdoptFunc != nil {
			m.canAdoptErr = m.CanAdoptFunc()
		}
	})
	return m.canAdoptErr
}

// ClaimObject tries to take ownership of an object for this controller.
//
// It will reconcile the following:
//   * Adopt orphans if the match function returns true.
//   * Release owned objects if the match function returns false.
//
// A non-nil error is returned if some form of reconciliation was attempted and
// failed. Usually, controllers should try again later in case reconciliation
// is still needed.
//
// If the error is nil, either the reconciliation succeeded, or no
// reconciliation was necessary. The returned boolean indicates whether you now
// own the object.
//
// No reconciliation will be attempted if the controller is being deleted.
func (m *BaseControllerRefManager) ClaimObject(obj metav1.Object, match func(metav1.Object) bool, adopt, release func(metav1.Object) error) (bool, error) {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef != nil {
		if controllerRef.UID != m.Controller.GetUID() {
			// Owned by someone else. Ignore.
			return false, nil
		}
		if match(obj) {
			// We already own it and the selector matches.
			// Return true (successfully claimed) before checking deletion timestamp.
			// We're still allowed to claim things we already own while being deleted
			// because doing so requires taking no actions.
			return true, nil
		}
		// Owned by us but selector doesn't match.
		// Try to release, unless we're being deleted.
		if m.Controller.GetDeletionTimestamp() != nil {
			return false, nil
		}
		if err := release(obj); err != nil {
			// If the pod no longer exists, ignore the error.
			if errors.IsNotFound(err) {
				return false, nil
			}
			// Either someone else released it, or there was a transient error.
			// The controller should requeue and try again if it's still stale.
			return false, err
		}
		// Successfully released.
		return false, nil
	}

	// It's an orphan.
	if m.Controller.GetDeletionTimestamp() != nil || !match(obj) {
		// Ignore if we're being deleted or selector doesn't match.
		return false, nil
	}
	if obj.GetDeletionTimestamp() != nil {
		// Ignore if the object is being deleted
		return false, nil
	}
	// Selector matches. Try to adopt.
	if err := adopt(obj); err != nil {
		// If the pod no longer exists, ignore the error.
		if errors.IsNotFound(err) {
			return false, nil
		}
		// Either someone else claimed it first, or there was a transient error.
		// The controller should requeue and try again if it's still orphaned.
		return false, err
	}
	// Successfully adopted.
	return true, nil
}
