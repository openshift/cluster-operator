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
	"math/rand"
	"testing"
)

func TestUIDExpectations(t *testing.T) {
	uidExp := NewUIDTrackingControllerExpectations(NewControllerExpectations())
	ownersList := []struct {
		name  string
		count int
	}{
		{
			name:  "first",
			count: 2,
		},
		{
			name:  "second",
			count: 1,
		},
		{
			name:  "third",
			count: 5,
		},
	}
	ownerToChild := map[string][]string{}
	ownerKeys := make([]string, len(ownersList))
	for i, owner := range ownersList {
		ownerKeys[i] = owner.name
		childNames := make([]string, owner.count)
		for j := 0; j < owner.count; j++ {
			childNames[j] = fmt.Sprint("%v-%v", owner.name, j)
		}
		ownerToChild[owner.name] = childNames
		uidExp.ExpectDeletions(owner.name, childNames)
	}
	for i := range ownerKeys {
		j := rand.Intn(i + 1)
		ownerKeys[i], ownerKeys[j] = ownerKeys[j], ownerKeys[i]
	}
	for _, ownerKey := range ownerKeys {
		if uidExp.SatisfiedExpectations(ownerKey) {
			t.Errorf("Controller %q satisfied expectations before deletion", ownerKey)
		}
		for _, c := range ownerToChild[ownerKey] {
			uidExp.DeletionObserved(ownerKey, c)
		}
		if !uidExp.SatisfiedExpectations(ownerKey) {
			t.Errorf("Controller %q didn't satisfy expectations after deletion", ownerKey)
		}
		uidExp.DeleteExpectations(ownerKey)
		if uidExp.GetUIDs(ownerKey) != nil {
			t.Errorf("Failed to delete uid expectations for %q", ownerKey)
		}
	}
}
