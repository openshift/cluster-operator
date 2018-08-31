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

// TestValidateNodeType validates the validateNodeType function.
func TestValidateNodeType(t *testing.T) {
	cases := []struct {
		nodeType string
		valid    bool
	}{
		{
			nodeType: "Master",
			valid:    true,
		},
		{
			nodeType: "Compute",
			valid:    true,
		},
		{
			nodeType: "",
			valid:    false,
		},
		{
			nodeType: "Other",
			valid:    false,
		},
	}

	for _, tc := range cases {
		errs := validateNodeType(coapi.NodeType(tc.nodeType), field.NewPath("nodeType"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.nodeType, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.nodeType)
		}
	}
}
