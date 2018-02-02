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

package controller

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"

	"github.com/golang/glog"
)

// UIDSetKeyFunc to parse out the key from a UIDSet.
var UIDSetKeyFunc = func(obj interface{}) (string, error) {
	if u, ok := obj.(*UIDSet); ok {
		return u.key, nil
	}
	return "", fmt.Errorf("Could not find key for obj %#v", obj)
}

// UIDSet holds a key and a set of UIDs. Used by the
// UIDTrackingExpectations to remember which UID it has seen/still
// waiting for.
type UIDSet struct {
	sets.String
	key string
}

// UIDTrackingExpectations tracks the UID of the resources it deletes.
// This cache is needed over plain old expectations to safely handle graceful
// deletion. The desired behavior is to treat an update that sets the
// DeletionTimestamp on an object as a delete. To do so consistently, one needs
// to remember the expected deletes so they aren't double counted.
// TODO: Track creates as well (#22599)
type UIDTrackingExpectations struct {
	ExpectationsInterface
	// TODO: There is a much nicer way to do this that involves a single store,
	// a lock per entry, and a ControlleeExpectationsInterface type.
	uidStoreLock sync.Mutex
	// Store used for the UIDs associated with any expectation tracked via the
	// ExpectationsInterface.
	uidStore cache.Store
}

// GetUIDs is a convenience method to avoid exposing the set of expected uids.
// The returned set is not thread safe, all modifications must be made holding
// the uidStoreLock.
func (u *UIDTrackingExpectations) GetUIDs(controllerKey string) sets.String {
	if uid, exists, err := u.uidStore.GetByKey(controllerKey); err == nil && exists {
		return uid.(*UIDSet).String
	}
	return nil
}

// SetExpectations registers new expectations for the given controller. Forgets existing expectations.
func (u *UIDTrackingExpectations) SetExpectations(controllerKey string, adds int, deletedKeys []string) error {
	u.uidStoreLock.Lock()
	defer u.uidStoreLock.Unlock()

	if existing := u.GetUIDs(controllerKey); existing != nil && existing.Len() != 0 {
		glog.Errorf("Clobbering existing delete keys: %+v", existing)
	}
	expectedUIDs := sets.NewString()
	for _, k := range deletedKeys {
		expectedUIDs.Insert(k)
	}
	glog.V(4).Infof("Controller %v waiting on deletions for: %+v", controllerKey, deletedKeys)
	if err := u.uidStore.Add(&UIDSet{expectedUIDs, controllerKey}); err != nil {
		return err
	}
	return u.ExpectationsInterface.SetExpectations(controllerKey, adds, expectedUIDs.Len())
}

// ExpectCreations records expectations for the given number of creations, against the given controller.
func (u *UIDTrackingExpectations) ExpectCreations(controllerKey string, adds int) error {
	return u.SetExpectations(controllerKey, adds, []string{})
}

// ExpectDeletions records expectations for the given deleteKeys, against the given controller.
func (u *UIDTrackingExpectations) ExpectDeletions(controllerKey string, deletedKeys []string) error {
	return u.SetExpectations(controllerKey, 0, deletedKeys)
}

// DeletionObserved records the given deleteKey as a deletion, for the given controller.
func (u *UIDTrackingExpectations) DeletionObserved(controllerKey, deleteKey string) {
	u.uidStoreLock.Lock()
	defer u.uidStoreLock.Unlock()

	uids := u.GetUIDs(controllerKey)
	if uids != nil && uids.Has(deleteKey) {
		glog.V(4).Infof("Controller %v received delete for pod %v", controllerKey, deleteKey)
		u.ExpectationsInterface.DeletionObserved(controllerKey)
		uids.Delete(deleteKey)
	}
}

// DeleteExpectations deletes the UID set and invokes DeleteExpectations on the
// underlying ExpectationsInterface.
func (u *UIDTrackingExpectations) DeleteExpectations(rcKey string) {
	u.uidStoreLock.Lock()
	defer u.uidStoreLock.Unlock()

	u.ExpectationsInterface.DeleteExpectations(rcKey)
	if uidExp, exists, err := u.uidStore.GetByKey(rcKey); err == nil && exists {
		if err := u.uidStore.Delete(uidExp); err != nil {
			glog.V(2).Infof("Error deleting uid expectations for controller %v: %v", rcKey, err)
		}
	}
}

// NewUIDTrackingExpectations returns a wrapper around
// Expectations that is aware of deleteKeys.
func NewUIDTrackingExpectations(ce ExpectationsInterface) *UIDTrackingExpectations {
	return &UIDTrackingExpectations{ExpectationsInterface: ce, uidStore: cache.NewStore(UIDSetKeyFunc)}
}
