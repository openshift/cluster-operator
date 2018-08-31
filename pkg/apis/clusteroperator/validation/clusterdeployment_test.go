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

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

// getValidClusterDeployment gets a cluster deployment that passes all validity checks.
func getValidClusterDeployment() *coapi.ClusterDeployment {
	return &coapi.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: getValidClusterDeploymentSpec(),
	}
}

func getValidClusterDeploymentSpec() coapi.ClusterDeploymentSpec {
	return coapi.ClusterDeploymentSpec{
		ClusterName: "cluster-name",
		MachineSets: []coapi.ClusterMachineSet{
			{
				MachineSetConfig: coapi.MachineSetConfig{
					NodeType: coapi.NodeTypeMaster,
					Infra:    true,
					Size:     1,
				},
			},
		},
		ClusterVersionRef: coapi.ClusterVersionReference{
			Name: "v3-9",
		},
		NetworkConfig: capiv1.ClusterNetworkingConfig{
			ServiceDomain: "test.svc.local",
			Services:      capiv1.NetworkRanges{CIDRBlocks: []string{"172.50.1.1/16"}},
			Pods:          capiv1.NetworkRanges{CIDRBlocks: []string{"10.140.5.5/14"}},
		},
		Config: coapi.ClusterConfigSpec{
			SDNPluginName: "redhat/openshift-ovs-multitenant",
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
func getTestMachineSet(size int, shortName string, master bool, infra bool) coapi.ClusterMachineSet {
	nodeType := coapi.NodeTypeCompute
	if master {
		nodeType = coapi.NodeTypeMaster
	}
	return coapi.ClusterMachineSet{
		ShortName: shortName,
		MachineSetConfig: coapi.MachineSetConfig{
			NodeType: nodeType,
			Size:     size,
			Infra:    infra,
		},
	}
}

// TestValidateClusterDeployment tests the ValidateCluster function.
func TestValidateClusterDeployment(t *testing.T) {
	cases := []struct {
		name              string
		clusterDeployment *coapi.ClusterDeployment
		valid             bool
	}{
		{
			name:              "valid",
			clusterDeployment: getValidClusterDeployment(),
			valid:             true,
		},
		{
			name: "invalid name",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Name = "###"
				return c
			}(),
			valid: false,
		},
		{
			name: "invalid spec",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.MachineSets[0].Size = 0
				return c
			}(),
			valid: false,
		},
		{
			name: "missing service network CIDRs",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.NetworkConfig.Services = capiv1.NetworkRanges{CIDRBlocks: []string{}}
				return c
			}(),
			valid: false,
		},
		{
			name: "missing pod network CIDRs",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.NetworkConfig.Pods = capiv1.NetworkRanges{CIDRBlocks: []string{}}
				return c
			}(),
			valid: false,
		},
		{
			// NOTE: this isn't supported yet
			name: "multiple service network CIDRs",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.NetworkConfig.Services = capiv1.NetworkRanges{CIDRBlocks: []string{"192.168.1.1/10", "196.168.1.2/20"}}
				return c
			}(),
			valid: false,
		},
		{
			// NOTE: this isn't supported yet
			name: "multiple pod network CIDRs",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.NetworkConfig.Pods = capiv1.NetworkRanges{CIDRBlocks: []string{"192.168.1.1/10", "196.168.1.2/20"}}
				return c
			}(),
			valid: false,
		},
		{
			name: "missing SDN plugin name",
			clusterDeployment: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.Config.SDNPluginName = ""
				return c
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateClusterDeployment(tc.clusterDeployment)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateClusterDeploymentUpdate tests the ValidateClusterDeploymentUpdate function.
func TestValidateClusterDeploymentUpdate(t *testing.T) {
	cases := []struct {
		name  string
		old   *coapi.ClusterDeployment
		new   *coapi.ClusterDeployment
		valid bool
	}{
		{
			name:  "valid",
			old:   getValidClusterDeployment(),
			new:   getValidClusterDeployment(),
			valid: true,
		},
		{
			name: "invalid spec",
			old:  getValidClusterDeployment(),
			new: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.MachineSets[0].Size = 0
				return c
			}(),
			valid: false,
		},
		{
			name: "mutated ClusterName",
			old:  getValidClusterDeployment(),
			new: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.ClusterName = "mutated-cluster-name"
				return c
			}(),
			valid: false,
		},
		{
			name: "mutated service network CIDR",
			old:  getValidClusterDeployment(),
			new: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.NetworkConfig.Services = capiv1.NetworkRanges{CIDRBlocks: []string{"172.60.0.0/16"}}
				return c
			}(),
			valid: false,
		},
		{
			name: "mutated pod network CIDR",
			old:  getValidClusterDeployment(),
			new: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.NetworkConfig.Pods = capiv1.NetworkRanges{CIDRBlocks: []string{"172.60.0.0/16"}}
				return c
			}(),
			valid: false,
		},
		{
			name: "mutated SDN plugin name",
			old:  getValidClusterDeployment(),
			new: func() *coapi.ClusterDeployment {
				c := getValidClusterDeployment()
				c.Spec.Config.SDNPluginName = "newplugin"
				return c
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateClusterDeploymentUpdate(tc.new, tc.old)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}

// TestValidateClusterDeploymentSpec tests the validateClusterDeploymentSpec function.
func TestValidateClusterDeploymentSpec(t *testing.T) {
	cases := []struct {
		name  string
		spec  *coapi.ClusterDeploymentSpec
		valid bool
	}{
		{
			name: "missing clusterID",
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.ClusterName = ""
				return &cs
			}(),
			valid: false,
		},
		{
			name: "valid master only",
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				return &cs
			}(),
			valid: true,
		},
		{
			name: "invalid master size",
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(0, "", true, true),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "valid single compute",
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, false),
					getTestMachineSet(1, "one", false, true),
				}
				return &cs
			}(),
			valid: true,
		},
		{
			name: "valid multiple computes",
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, true),
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, true),
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, true),
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, true),
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, false),
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, true),
					getTestMachineSet(1, "", true, false),
					getTestMachineSet(5, "one", false, false),
					getTestMachineSet(2, "two", false, false),
				}
				return &cs
			}(),
			valid: false,
		},
		{
			name: "more than one infra",
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.MachineSets = []coapi.ClusterMachineSet{
					getTestMachineSet(1, "", true, false),
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
			spec: func() *coapi.ClusterDeploymentSpec {
				cs := getValidClusterDeploymentSpec()
				cs.ClusterVersionRef.Name = ""
				return &cs
			}(),
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := validateClusterDeploymentSpec(tc.spec, field.NewPath("spec"))
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
