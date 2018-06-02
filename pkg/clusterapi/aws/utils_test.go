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

package aws

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/service/ec2"
)

func TestSortInstances(t *testing.T) {
	cases := []struct {
		name                        string
		instances                   []*ec2.Instance
		expectedActiveInstanceID    string
		expectedInactiveInstanceIDs []string
	}{
		{
			name: "only one",
			instances: []*ec2.Instance{
				buildInstance("i1", time.Now().Add(time.Minute*60)),
			},
			expectedActiveInstanceID:    "i1",
			expectedInactiveInstanceIDs: []string{},
		},
		{
			name:                        "no instances given",
			instances:                   []*ec2.Instance{},
			expectedActiveInstanceID:    "",
			expectedInactiveInstanceIDs: []string{},
		},
		{
			name: "multiple running instances",
			instances: []*ec2.Instance{
				buildInstance("i1", time.Now().Add(-time.Minute*60)),
				buildInstance("i2", time.Now().Add(-time.Minute*10)),
				buildInstance("i3", time.Now().Add(-time.Minute*30)),
				buildInstance("i4", time.Now().Add(-time.Minute*90)),
			},
			expectedActiveInstanceID:    "i2",
			expectedInactiveInstanceIDs: []string{"i1", "i3", "i4"},
		},
		{
			name: "some instances missing launchtime",
			instances: []*ec2.Instance{
				buildInstance("i1", time.Now().Add(-time.Minute*60)),
				buildInstance("i2", time.Time{}),
				buildInstance("i3", time.Now().Add(-time.Minute*30)),
				buildInstance("i4", time.Time{}),
			},
			expectedActiveInstanceID:    "i3",
			expectedInactiveInstanceIDs: []string{"i1", "i2", "i4"},
		},
		{
			name: "no instances with launchtime",
			instances: []*ec2.Instance{
				buildInstance("i1", time.Time{}),
				buildInstance("i2", time.Time{}),
				buildInstance("i3", time.Time{}),
				buildInstance("i4", time.Time{}),
			},
			expectedActiveInstanceID:    "i1",
			expectedInactiveInstanceIDs: []string{"i2", "i3", "i4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			activeInstance, inactiveInstances := SortInstances(tc.instances)
			if tc.expectedActiveInstanceID != "" {
				assert.Equal(t, tc.expectedActiveInstanceID, *activeInstance.InstanceId)
			}
			assert.Equal(t, len(tc.expectedInactiveInstanceIDs), len(inactiveInstances))
			for _, iID := range tc.expectedInactiveInstanceIDs {
				var found bool
				for _, instance := range inactiveInstances {
					if *instance.InstanceId == iID {
						found = true
					}
				}
				assert.True(t, found, "instance %s was not found in inactive instances", iID)
			}
		})
	}
}

func buildInstance(instanceID string, launchTime time.Time) *ec2.Instance {
	instance := &ec2.Instance{
		InstanceId: &instanceID,
	}
	// Use a zero value launchTime as an indicator for no launch time on the instance.
	if !launchTime.Equal(time.Time{}) {
		instance.LaunchTime = &launchTime
	}
	return instance
}
