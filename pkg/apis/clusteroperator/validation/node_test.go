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

// getValidNode gets a node that passes all validity checks.
func getValidNode() *clusteroperator.Node {
	return &clusteroperator.Node{
		Spec: clusteroperator.NodeSpec{
			NodeType: clusteroperator.NodeTypeMaster,
		},
	}
}

// TestValidateNode tests the ValidateNode function.
func TestValidateNode(t *testing.T) {
	cases := []struct {
		name  string
		node  *clusteroperator.Node
		valid bool
	}{
		{
			name:  "valid",
			node:  getValidNode(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateNode(tc.node)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeUpdate tests the ValidateNodeUpdate function.
func TestValidateNodeUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.Node
		new   *clusteroperator.Node
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidNode(),
			new:   getValidNode(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateNodeUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeStatusUpdate tests the ValidateNodeStatus function.
func TestValidateNodeStatusUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.Node
		new   *clusteroperator.Node
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidNode(),
			new:   getValidNode(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateNodeStatusUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeSpec tests the validateNodeSpec function.
func TestValidateNodeSpec(t *testing.T) {
	cases := []struct {
		name  string
		spec  *clusteroperator.NodeSpec
		valid bool
	}{
		{
			name: "valid",
			spec: &clusteroperator.NodeSpec{
				NodeType: clusteroperator.NodeTypeMaster,
			},
			valid: true,
		},
		{
			name:  "invalid node type",
			spec:  &clusteroperator.NodeSpec{},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateNodeSpec(tc.spec, field.NewPath("spec"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeStatus tests the validateNodeStatus function.
func TestValidateNodeStatus(t *testing.T) {
	cases := []struct {
		name   string
		status *clusteroperator.NodeStatus
		valid  bool
	}{
		{
			name:   "valid",
			status: &clusteroperator.NodeStatus{},
			valid:  true,
		},
	}

	for _, tc := range cases {
		errs := validateNodeStatus(tc.status, field.NewPath("status"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
