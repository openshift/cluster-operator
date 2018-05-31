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
		name                    string
		instances               []*ec2.Instance
		expectedFirstInstanceID string
	}{
		{
			name: "only one",
			instances: []*ec2.Instance{
				buildInstance("i1", time.Now().Add(time.Minute*60)),
			},
			expectedFirstInstanceID: "i1",
		},
		{
			name: "multiple running instances",
			instances: []*ec2.Instance{
				buildInstance("i1", time.Now().Add(-time.Minute*60)),
				buildInstance("i2", time.Now().Add(-time.Minute*10)),
				buildInstance("i3", time.Now().Add(-time.Minute*30)),
				buildInstance("i4", time.Now().Add(-time.Minute*90)),
			},
			expectedFirstInstanceID: "i2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SortInstances(tc.instances)
			assert.Equal(t, tc.expectedFirstInstanceID, *tc.instances[0].InstanceId)
		})
	}
}

func buildInstance(instanceID string, launchTime time.Time) *ec2.Instance {
	return &ec2.Instance{
		InstanceId: &instanceID,
		LaunchTime: &launchTime,
	}
}
