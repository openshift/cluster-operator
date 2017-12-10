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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// getValidClusterOwnerRef returns an owner reference that can be used as a
// valid cluster owner reference for a node group.
func getValidClusterOwnerRef() metav1.OwnerReference {
	truePtr := func() *bool { b := true; return &b }
	return metav1.OwnerReference{
		APIVersion: "clusteroperator.openshift.io/v1alpha1",
		Kind:       "Cluster",
		UID:        "cluster-owner",
		Controller: truePtr(),
	}
}

// getValidNodeGroup gets a node group that passes all validity checks.
func getValidNodeGroup() *clusteroperator.NodeGroup {
	return &clusteroperator.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{getValidClusterOwnerRef()},
		},
		Spec: clusteroperator.NodeGroupSpec{
			NodeType: clusteroperator.NodeTypeMaster,
			Size:     1,
		},
	}
}

// TestValidateNodeGroup tests the ValidateNodeGroup function.
func TestValidateNodeGroup(t *testing.T) {
	cases := []struct {
		name      string
		nodeGroup *clusteroperator.NodeGroup
		valid     bool
	}{
		{
			name:      "valid",
			nodeGroup: getValidNodeGroup(),
			valid:     true,
		},
		{
			name: "invalid cluster owner",
			nodeGroup: func() *clusteroperator.NodeGroup {
				ng := getValidNodeGroup()
				ng.OwnerReferences = []metav1.OwnerReference{}
				return ng
			}(),
			valid: false,
		},
		{
			name: "invalid spec",
			nodeGroup: func() *clusteroperator.NodeGroup {
				ng := getValidNodeGroup()
				ng.Spec.Size = 0
				return ng
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateNodeGroup(tc.nodeGroup)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeGroupUpdate tests the ValidateNodeGroupUpdate function.
func TestValidateNodeGroupUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.NodeGroup
		new   *clusteroperator.NodeGroup
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidNodeGroup(),
			new:   getValidNodeGroup(),
			valid: true,
		},
		{
			name: "invalid cluster owner mutation",
			old: func() *clusteroperator.NodeGroup {
				ng := getValidNodeGroup()
				ng.OwnerReferences[0].UID = "old-owner-uid"
				return ng
			}(),
			new: func() *clusteroperator.NodeGroup {
				ng := getValidNodeGroup()
				ng.OwnerReferences[0].UID = "new-owner-uid"
				return ng
			}(),
			valid: false,
		},
		{
			name: "invalid spec",
			old:  getValidNodeGroup(),
			new: func() *clusteroperator.NodeGroup {
				ng := getValidNodeGroup()
				ng.Spec.Size = 0
				return ng
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateNodeGroupUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeGroupStatusUpdate tests the ValidateNodeGroupStatusUpdate
// function.
func TestValidateNodeGroupStatusUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.NodeGroup
		new   *clusteroperator.NodeGroup
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidNodeGroup(),
			new:   getValidNodeGroup(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateNodeGroupStatusUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeGroupClusterOwner tests the validateNodeGroupClusterOwner
// function.
func TestValidateNodeGroupClusterOwner(t *testing.T) {
	cases := []struct {
		name      string
		ownerRefs []metav1.OwnerReference
		valid     bool
	}{
		{
			name: "valid single owner",
			ownerRefs: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
			},
			valid: true,
		},
		{
			name: "valid multiple owners",
			ownerRefs: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
				{},
			},
			valid: true,
		},
		{
			name:      "no owners",
			ownerRefs: []metav1.OwnerReference{},
			valid:     false,
		},
		{
			name: "first owner not cluster owner",
			ownerRefs: []metav1.OwnerReference{
				func() metav1.OwnerReference {
					r := getValidClusterOwnerRef()
					r.Kind = "other-kind"
					return r
				}(),
				getValidClusterOwnerRef(),
			},
			valid: false,
		},
		{
			name: "invalid api version",
			ownerRefs: []metav1.OwnerReference{
				func() metav1.OwnerReference {
					r := getValidClusterOwnerRef()
					r.APIVersion = "other-api-version"
					return r
				}(),
			},
			valid: false,
		},
		{
			name: "invalid kind",
			ownerRefs: []metav1.OwnerReference{
				func() metav1.OwnerReference {
					r := getValidClusterOwnerRef()
					r.Kind = "other-kind"
					return r
				}(),
			},
			valid: false,
		},
		{
			name: "cluster owner not controller",
			ownerRefs: []metav1.OwnerReference{
				func() metav1.OwnerReference {
					r := getValidClusterOwnerRef()
					r.Controller = nil
					return r
				}(),
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateNodeGroupClusterOwner(tc.ownerRefs, field.NewPath("metadata").Child("ownerReferences"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeGroupSpec tests the validateNodeGroupSpec function.
func TestValidateNodeGroupSpec(t *testing.T) {
	cases := []struct {
		name  string
		spec  *clusteroperator.NodeGroupSpec
		valid bool
	}{
		{
			name: "valid",
			spec: &clusteroperator.NodeGroupSpec{
				NodeType: clusteroperator.NodeTypeMaster,
				Size:     1,
			},
			valid: true,
		},
		{
			name: "invalid node type",
			spec: &clusteroperator.NodeGroupSpec{
				NodeType: clusteroperator.NodeType(""),
				Size:     1,
			},
		},
		{
			name: "invalid size",
			spec: &clusteroperator.NodeGroupSpec{
				NodeType: clusteroperator.NodeTypeMaster,
				Size:     0,
			},
		},
	}

	for _, tc := range cases {
		errs := validateNodeGroupSpec(tc.spec, field.NewPath("spec"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeGroupImmutableClusterOwner tests the
// validateNodeGroupImmutableClusterOwner function.
func TestValidateNodeGroupImmutableClusterOwner(t *testing.T) {
	cases := []struct {
		name  string
		old   []metav1.OwnerReference
		new   []metav1.OwnerReference
		valid bool
	}{
		{
			name: "valid single owner",
			old: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
			},
			new: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
			},
			valid: true,
		},
		{
			name: "valid multiple owners",
			old: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
				{},
			},
			new: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
				{},
			},
			valid: true,
		},
		{
			name: "single owner removed",
			old: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
			},
			new:   []metav1.OwnerReference{},
			valid: false,
		},
		{
			name: "cluster owner removed",
			old: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
				{},
			},
			new: []metav1.OwnerReference{
				{},
				{},
			},
			valid: false,
		},
		{
			name: "cluster owner moved",
			old: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
				{},
			},
			new: []metav1.OwnerReference{
				{},
				getValidClusterOwnerRef(),
				{},
			},
			valid: false,
		},
		{
			name: "other owner removed",
			old: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
				{},
			},
			new: []metav1.OwnerReference{
				getValidClusterOwnerRef(),
				{},
			},
			valid: true,
		},
		{
			name: "cluster owner mutated",
			old: []metav1.OwnerReference{
				func() metav1.OwnerReference {
					r := getValidClusterOwnerRef()
					r.UID = "old-owner-uid"
					return r
				}(),
			},
			new: []metav1.OwnerReference{
				func() metav1.OwnerReference {
					r := getValidClusterOwnerRef()
					r.UID = "new-owner-uid"
					return r
				}(),
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateNodeGroupImmutableClusterOwner(tc.new, tc.old, field.NewPath("metadata").Child("ownerReferences"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateNodeGroupStatus tests the validateNodeGroupStatus function.
func TestValidateNodeGroupStatus(t *testing.T) {
	cases := []struct {
		name   string
		status *clusteroperator.NodeGroupStatus
		valid  bool
	}{}

	for _, tc := range cases {
		errs := validateNodeGroupStatus(tc.status, field.NewPath("status"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
