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
// valid cluster owner reference for a machine set.
func getValidClusterOwnerRef() metav1.OwnerReference {
	truePtr := func() *bool { b := true; return &b }
	return metav1.OwnerReference{
		APIVersion: "clusteroperator.openshift.io/v1alpha1",
		Kind:       "Cluster",
		UID:        "cluster-owner",
		Controller: truePtr(),
	}
}

// getValidMachineSet gets a machine set that passes all validity checks.
func getValidMachineSet() *clusteroperator.MachineSet {
	return &clusteroperator.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{getValidClusterOwnerRef()},
		},
		Spec: clusteroperator.MachineSetSpec{
			clusteroperator.MachineSetConfig{
				NodeType: clusteroperator.NodeTypeMaster,
				Size:     1,
			},
		},
	}
}

// TestValidateMachineSet tests the ValidateMachineSet function.
func TestValidateMachineSet(t *testing.T) {
	cases := []struct {
		name       string
		machineSet *clusteroperator.MachineSet
		valid      bool
	}{
		{
			name:       "valid",
			machineSet: getValidMachineSet(),
			valid:      true,
		},
		{
			name: "invalid cluster owner",
			machineSet: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{}
				return ms
			}(),
			valid: false,
		},
		{
			name: "invalid spec",
			machineSet: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.Spec.Size = 0
				return ms
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateMachineSet(tc.machineSet)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineSetUpdate tests the ValidateMachineSetUpdate function.
func TestValidateMachineSetUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.MachineSet
		new   *clusteroperator.MachineSet
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidMachineSet(),
			new:   getValidMachineSet(),
			valid: true,
		},
		{
			name: "invalid spec",
			old:  getValidMachineSet(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.Spec.Size = 0
				return ms
			}(),
			valid: false,
		},
		{
			name: "valid single owner",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
				}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
				}
				return ms
			}(),
			valid: true,
		},
		{
			name: "valid multiple owners",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
					{},
					{},
				}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
					{},
					{},
				}
				return ms
			}(),
			valid: true,
		},
		{
			name: "single owner removed",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
				}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{}
				return ms
			}(),
			valid: false,
		},
		{
			name: "cluster owner removed",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
					{},
					{},
				}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					{},
					{},
				}
				return ms
			}(),
			valid: false,
		},
		{
			name: "cluster owner moved",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
					{},
					{},
				}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					{},
					getValidClusterOwnerRef(),
					{},
				}
				return ms
			}(),
			valid: true,
		},
		{
			name: "other owner removed",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
					{},
					{},
				}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				ms.OwnerReferences = []metav1.OwnerReference{
					getValidClusterOwnerRef(),
					{},
				}
				return ms
			}(),
			valid: true,
		},
		{
			name: "cluster owner mutated",
			old: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				owner := getValidClusterOwnerRef()
				owner.UID = "old-owner-uid"
				ms.OwnerReferences = []metav1.OwnerReference{owner}
				return ms
			}(),
			new: func() *clusteroperator.MachineSet {
				ms := getValidMachineSet()
				owner := getValidClusterOwnerRef()
				owner.UID = "new-owner-uid"
				ms.OwnerReferences = []metav1.OwnerReference{owner}
				return ms
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateMachineSetUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineSetStatusUpdate tests the ValidateMachineSetStatusUpdate
// function.
func TestValidateMachineSetStatusUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.MachineSet
		new   *clusteroperator.MachineSet
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidMachineSet(),
			new:   getValidMachineSet(),
			valid: true,
		},
	}

	for _, tc := range cases {
		errs := ValidateMachineSetStatusUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineSetClusterOwner tests the validateMachineSetClusterOwner
// function.
func TestValidateMachineSetClusterOwner(t *testing.T) {
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
			name: "non-first controlling owner",
			ownerRefs: []metav1.OwnerReference{
				{},
				getValidClusterOwnerRef(),
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
		machineSet := &clusteroperator.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: tc.ownerRefs,
			},
		}
		errs := validateMachineSetClusterOwner(machineSet)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineSetSpec tests the validateMachineSetSpec function.
func TestValidateMachineSetSpec(t *testing.T) {
	cases := []struct {
		name  string
		spec  *clusteroperator.MachineSetSpec
		valid bool
	}{
		{
			name: "valid",
			spec: &clusteroperator.MachineSetSpec{
				clusteroperator.MachineSetConfig{
					NodeType: clusteroperator.NodeTypeMaster,
					Size:     1,
				},
			},
			valid: true,
		},
		{
			name: "invalid node type",
			spec: &clusteroperator.MachineSetSpec{
				clusteroperator.MachineSetConfig{
					NodeType: clusteroperator.NodeType(""),
					Size:     1,
				},
			},
		},
		{
			name: "invalid size",
			spec: &clusteroperator.MachineSetSpec{
				clusteroperator.MachineSetConfig{
					NodeType: clusteroperator.NodeTypeMaster,
					Size:     0,
				},
			},
		},
	}

	for _, tc := range cases {
		errs := validateMachineSetSpec(tc.spec, field.NewPath("spec"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateMachineSetStatus tests the validateMachineSetStatus function.
func TestValidateMachineSetStatus(t *testing.T) {
	cases := []struct {
		name   string
		status *clusteroperator.MachineSetStatus
		valid  bool
	}{}

	for _, tc := range cases {
		errs := validateMachineSetStatus(tc.status, field.NewPath("status"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
