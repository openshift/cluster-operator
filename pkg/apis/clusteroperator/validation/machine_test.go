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

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// getValidMachine gets a machine that passes all validity checks.
func getValidMachine() *clusteroperator.Machine {
	return &clusteroperator.Machine{
		Spec: clusteroperator.MachineSpec{
			NodeType: clusteroperator.NodeTypeMaster,
		},
	}
}

// TestValidateMachine tests the ValidateMachine function.
func TestValidateMachine(t *testing.T) {
	cases := []struct {
		name    string
		machine *clusteroperator.Machine
		valid   bool
	}{
		{
			name:    "valid",
			machine: getValidMachine(),
			valid:   true,
		},
	}

	for _, tc := range cases {
		errs := ValidateMachine(tc.machine)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineUpdate tests the ValidateMachineUpdate function.
func TestValidateMachineUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.Machine
		new   *clusteroperator.Machine
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidMachine(),
			new:   getValidMachine(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateMachineUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineStatusUpdate tests the ValidateMachineStatus function.
func TestValidateMachineStatusUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.Machine
		new   *clusteroperator.Machine
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidMachine(),
			new:   getValidMachine(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateMachineStatusUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineSpec tests the validateMachineSpec function.
func TestValidateMachineSpec(t *testing.T) {
	cases := []struct {
		name  string
		spec  *clusteroperator.MachineSpec
		valid bool
	}{
		{
			name: "valid",
			spec: &clusteroperator.MachineSpec{
				NodeType: clusteroperator.NodeTypeMaster,
			},
			valid: true,
		},
		{
			name:  "invalid machine type",
			spec:  &clusteroperator.MachineSpec{},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateMachineSpec(tc.spec, field.NewPath("spec"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineStatus tests the validateMachineStatus function.
func TestValidateMachineStatus(t *testing.T) {
	cases := []struct {
		name   string
		status *clusteroperator.MachineStatus
		valid  bool
	}{
		{
			name:   "valid",
			status: &clusteroperator.MachineStatus{},
			valid:  true,
		},
	}

	for _, tc := range cases {
		errs := validateMachineStatus(tc.status, field.NewPath("status"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
