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

package validation

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// TestValidateMachineSetConfig tests the validateMachineSetConfig function.
func TestValidateMachineSetConfig(t *testing.T) {
	cases := []struct {
		name   string
		config *coapi.MachineSetConfig
		valid  bool
	}{
		{
			name: "valid",
			config: &coapi.MachineSetConfig{
				NodeType: coapi.NodeTypeMaster,
				Size:     1,
			},
			valid: true,
		},
		{
			name: "invalid node type",
			config: &coapi.MachineSetConfig{
				NodeType: coapi.NodeType(""),
				Size:     1,
			},
		},
		{
			name: "invalid size",
			config: &coapi.MachineSetConfig{
				NodeType: coapi.NodeTypeMaster,
				Size:     0,
			},
		},
	}

	for _, tc := range cases {
		errs := validateMachineSetConfig(tc.config, field.NewPath("config"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
