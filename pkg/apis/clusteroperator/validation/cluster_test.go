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

	corev1 "k8s.io/api/core/v1"
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
		Spec: getValidClusterSpec(),
	}
}

func getValidClusterSpec() clusteroperator.ClusterSpec {
	return clusteroperator.ClusterSpec{
		MachineSets: []clusteroperator.ClusterMachineSet{
			{
				ShortName: "master",
				MachineSetConfig: clusteroperator.MachineSetConfig{
					NodeType: clusteroperator.NodeTypeMaster,
					Infra:    true,
					Size:     1,
				},
			},
		},
		ClusterVersionRef: clusteroperator.ClusterVersionReference{
			Name: "v3-9",
		},
	}
}

func getClusterVersionReference() corev1.ObjectReference {
	return corev1.ObjectReference{
		Namespace: "openshift-cluster-operator",
		Name:      "v3-9",
		UID:       "fakeuid",
	}
}

// getTestMachineSet gets a ClusterMachineSet initialized with either compute or master node type
func getTestMachineSet(size int, shortName string, master bool, infra bool) clusteroperator.ClusterMachineSet {
	nodeType := clusteroperator.NodeTypeCompute
	if master {
		nodeType = clusteroperator.NodeTypeMaster
	}
	return clusteroperator.ClusterMachineSet{
		ShortName: shortName,
		MachineSetConfig: clusteroperator.MachineSetConfig{
			NodeType: nodeType,
			Size:     size,
			Infra:    infra,
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
				c.Spec.MachineSets[0].Size = 0
				return c
			}(),
			valid: false,
		},
		{
			name: "invalid status",
			cluster: func() *clusteroperator.Cluster {
				c := getValidCluster()
				c.Status.MachineSetCount = -1
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
				c.Spec.MachineSets[0].Size = 0
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
				c.Status.MachineSetCount = -1
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
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				return &cs
			}(),
			valid: true,
		},
		{
			name: "invalid master size",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(0, "master", true, true),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "valid single compute",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master", true, false),
					getTestMachineSet(1, "one", false, true),
				}
				return &cs
			}(),
			valid: true,
		},
		{
			name: "valid multiple computes",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master", true, true),
					getTestMachineSet(1, "one", false, false),
					getTestMachineSet(5, "two", false, false),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: true,
		},
		{
			name: "invalid compute name",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master", true, true),
					getTestMachineSet(1, "one", false, false),
					getTestMachineSet(5, "", false, false),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "invalid compute size",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master", true, true),
					getTestMachineSet(1, "one", false, false),
					getTestMachineSet(0, "two", false, false),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "invalid duplicate compute name",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master", true, true),
					getTestMachineSet(1, "one", false, false),
					getTestMachineSet(5, "one", false, false),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "no master machineset",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "one", false, true),
					getTestMachineSet(5, "two", false, false),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "no infra machineset",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master", true, false),
					getTestMachineSet(1, "one", false, false),
					getTestMachineSet(5, "one", false, false),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "more than one master",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master1", true, true),
					getTestMachineSet(1, "master2", true, false),
					getTestMachineSet(5, "one", false, false),
					getTestMachineSet(2, "two", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "more than one infra",
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.MachineSets = []clusteroperator.ClusterMachineSet{
					getTestMachineSet(1, "master1", true, false),
					getTestMachineSet(1, "one", false, true),
					getTestMachineSet(5, "two", false, true),
					getTestMachineSet(2, "three", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "missing cluster version name", // namespace is optional
			spec: func() *clusteroperator.ClusterSpec {
				cs := getValidClusterSpec()
				cs.ClusterVersionRef.Name = ""
				return &cs
			}(),
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
			name: "positive machinesets",
			status: &clusteroperator.ClusterStatus{
				MachineSetCount: 1,
			},
			valid: true,
		},
		{
			name: "negative machinesets",
			status: &clusteroperator.ClusterStatus{
				MachineSetCount: -1,
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
