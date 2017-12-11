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

// getValidCluster gets a cluster that passes all validity checks.
func getValidCluster() *clusteroperator.Cluster {
	return &clusteroperator.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: clusteroperator.ClusterSpec{
			MasterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
		},
	}
}

// getTestClusterComputeNodeGroup gets a ClusterComputeNodeGroup with the
// specified name and size.
func getTestClusterComputeNodeGroup(name string, size int) clusteroperator.ClusterComputeNodeGroup {
	return clusteroperator.ClusterComputeNodeGroup{
		Name: name,
		ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
			Size: size,
		},
	}
}

// TestValidateCluster tests the ValidateCluster function.
func TestValidateCluster(t *testing.T) {
	cases := []struct {
		name    string
		cluster *clusteroperator.Cluster
		valid   bool
	}{
		{
			name:    "valid",
			cluster: getValidCluster(),
			valid:   true,
		},
		{
			name: "invalid name",
			cluster: func() *clusteroperator.Cluster {
				c := getValidCluster()
				c.Name = "###"
				return c
			}(),
			valid: false,
		},
		{
			name: "invalid spec",
			cluster: func() *clusteroperator.Cluster {
				c := getValidCluster()
				c.Spec.MasterNodeGroup.Size = 0
				return c
			}(),
			valid: false,
		},
		{
			name: "invalid status",
			cluster: func() *clusteroperator.Cluster {
				c := getValidCluster()
				c.Status.MasterNodeGroups = -1
				return c
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateCluster(tc.cluster)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateClusterUpdate tests the ValidateClusterUpdate function.
func TestValidateClusterUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.Cluster
		new   *clusteroperator.Cluster
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidCluster(),
			new:   getValidCluster(),
			valid: true,
		},
		{
			name: "invalid spec",
			old:  getValidCluster(),
			new: func() *clusteroperator.Cluster {
				c := getValidCluster()
				c.Spec.MasterNodeGroup.Size = 0
				return c
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateClusterUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateClusterStatusUpdate tests the ValidateClusterStatusUpdate function.
func TestValidateClusterStatusUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *clusteroperator.Cluster
		new   *clusteroperator.Cluster
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidCluster(),
			new:   getValidCluster(),
			valid: true,
		},
		{
			name: "invalid status",
			old:  getValidCluster(),
			new: func() *clusteroperator.Cluster {
				c := getValidCluster()
				c.Status.MasterNodeGroups = -1
				return c
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateClusterStatusUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateClusterSpec tests the validateClusterSpec function.
func TestValidateClusterSpec(t *testing.T) {
	cases := []struct {
		name  string
		spec  *clusteroperator.ClusterSpec
		valid bool
	}{
		{
			name: "valid master only",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 1,
				},
			},
			valid: true,
		},
		{
			name: "invalid master size",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 0,
				},
			},
			valid: false,
		},
		{
			name: "valid single compute",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 1,
				},
				ComputeNodeGroups: []clusteroperator.ClusterComputeNodeGroup{
					getTestClusterComputeNodeGroup("first", 1),
				},
			},
			valid: true,
		},
		{
			name: "valid multiple computes",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 1,
				},
				ComputeNodeGroups: []clusteroperator.ClusterComputeNodeGroup{
					getTestClusterComputeNodeGroup("first", 1),
					getTestClusterComputeNodeGroup("second", 5),
					getTestClusterComputeNodeGroup("third", 2),
				},
			},
			valid: true,
		},
		{
			name: "invalid compute name",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 1,
				},
				ComputeNodeGroups: []clusteroperator.ClusterComputeNodeGroup{
					getTestClusterComputeNodeGroup("first", 1),
					getTestClusterComputeNodeGroup("", 5),
					getTestClusterComputeNodeGroup("third", 2),
				},
			},
			valid: false,
		},
		{
			name: "invalid compute size",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 1,
				},
				ComputeNodeGroups: []clusteroperator.ClusterComputeNodeGroup{
					getTestClusterComputeNodeGroup("first", 1),
					getTestClusterComputeNodeGroup("second", 0),
					getTestClusterComputeNodeGroup("third", 2),
				},
			},
			valid: false,
		},
		{
			name: "invalid duplicate compute name",
			spec: &clusteroperator.ClusterSpec{
				MasterNodeGroup: clusteroperator.ClusterNodeGroup{
					Size: 1,
				},
				ComputeNodeGroups: []clusteroperator.ClusterComputeNodeGroup{
					getTestClusterComputeNodeGroup("first", 1),
					getTestClusterComputeNodeGroup("first", 5),
					getTestClusterComputeNodeGroup("third", 2),
				},
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateClusterSpec(tc.spec, field.NewPath("spec"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateClusterStatus tests the validateClusterStatus function.
func TestValidateClusterStatus(t *testing.T) {
	cases := []struct {
		name   string
		status *clusteroperator.ClusterStatus
		valid  bool
	}{
		{
			name:   "empty",
			status: &clusteroperator.ClusterStatus{},
			valid:  true,
		},
		{
			name: "positive masters",
			status: &clusteroperator.ClusterStatus{
				MasterNodeGroups: 1,
			},
			valid: true,
		},
		{
			name: "positive computes",
			status: &clusteroperator.ClusterStatus{
				ComputeNodeGroups: 1,
			},
			valid: true,
		},
		{
			name: "positive masters and computes",
			status: &clusteroperator.ClusterStatus{
				MasterNodeGroups:  1,
				ComputeNodeGroups: 1,
			},
			valid: true,
		},
		{
			name: "negative masters",
			status: &clusteroperator.ClusterStatus{
				MasterNodeGroups: -1,
			},
			valid: false,
		},
		{
			name: "negative computes",
			status: &clusteroperator.ClusterStatus{
				ComputeNodeGroups: -1,
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateClusterStatus(tc.status, field.NewPath("status"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
