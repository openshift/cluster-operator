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

package ansible

import (
	"testing"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	"github.com/stretchr/testify/assert"
)

func testCluster() *coapi.Cluster {
	cluster := &coapi.Cluster{}
	cluster.Name = "testcluster"
	cluster.Spec.Hardware.AWS = &coapi.AWSClusterSpec{
		Region:      "east-99",
		KeyPairName: "mykey",
	}
	return cluster
}

func testMachineSet() *coapi.MachineSet {
	ms := &coapi.MachineSet{}
	ms.Name = "testmachineset"
	ms.Spec.NodeType = coapi.NodeTypeCompute
	ms.Spec.Infra = false
	ms.Spec.Size = 3
	ms.Spec.Hardware = &coapi.MachineSetHardwareSpec{
		AWS: &coapi.MachineSetAWSHardwareSpec{
			InstanceType: "x9large",
			AMIName:      "myami",
		},
	}
	return ms
}

func TestGenerateClusterVars(t *testing.T) {
	tests := []struct {
		name             string
		cluster          *coapi.Cluster
		shouldInclude    []string
		shouldNotInclude []string
	}{
		{
			name:    "cluster with no default AMI name",
			cluster: testCluster(),
			shouldInclude: []string{
				"openshift_aws_clusterid: testcluster",
				"openshift_aws_vpc_name: testcluster",
				"openshift_aws_elb_basename: testcluster",
				"openshift_aws_ssh_key_name: mykey",
				"openshift_aws_region: east-99",
			},
			shouldNotInclude: []string{
				"openshift_aws_ami:",
			},
		},
		{
			name: "cluster with default AMI name",
			cluster: func() *coapi.Cluster {
				c := testCluster()
				c.Spec.DefaultHardwareSpec = &coapi.MachineSetHardwareSpec{
					AWS: &coapi.MachineSetAWSHardwareSpec{
						AMIName: "myami",
					},
				}
				return c
			}(),
			shouldInclude: []string{
				"openshift_aws_clusterid: testcluster",
				"openshift_aws_vpc_name: testcluster",
				"openshift_aws_elb_basename: testcluster",
				"openshift_aws_ssh_key_name: mykey",
				"openshift_aws_region: east-99",
				"openshift_aws_ami: myami",
			},
		},
	}

	for _, tc := range tests {
		result, err := GenerateClusterVars(tc.cluster)
		assert.Nil(t, err, "%s: unexpected: %v", tc.name, err)
		for _, str := range tc.shouldInclude {
			assert.Contains(t, result, str, "%s: result does not contain %q", tc.name, str)
		}
		for _, str := range tc.shouldNotInclude {
			assert.NotContains(t, result, str, "%s: result contains %q", tc.name, str)
		}
	}
}

func TestGenerateMachineSetVars(t *testing.T) {
	tests := []struct {
		name             string
		cluster          *coapi.Cluster
		machineSet       *coapi.MachineSet
		shouldInclude    []string
		shouldNotInclude []string
	}{
		{
			name:    "master machineset",
			cluster: testCluster(),
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.NodeType = coapi.NodeTypeMaster
				return ms
			}(),
			shouldInclude: []string{
				"master: myami",
				"instance_type: x9large",
				"desired_size: 3",
				"openshift_aws_clusterid: testcluster",
			},
		},
		{
			name:    "infra machineset",
			cluster: testCluster(),
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.Infra = true
				ms.Spec.Size = 5
				return ms
			}(),
			shouldInclude: []string{
				"infra: myami",
				"instance_type: x9large",
				"desired_size: 5",
				"openshift_aws_clusterid: testcluster",
				"sub-host-type: infra",
			},
		},
		{
			name:       "compute machineset",
			cluster:    testCluster(),
			machineSet: testMachineSet(),
			shouldInclude: []string{
				"compute: myami",
				"group: compute",
				"sub-host-type: compute",
				"instance_type: x9large",
				"desired_size: 3",
			},
		},
	}

	for _, tc := range tests {
		result, err := GenerateMachineSetVars(tc.cluster, tc.machineSet)
		assert.Nil(t, err, "%s: unexpected: %v", tc.name, err)
		for _, str := range tc.shouldInclude {
			assert.Contains(t, result, str, "%s: result does not contain %q", tc.name, str)
		}
		for _, str := range tc.shouldNotInclude {
			assert.NotContains(t, result, str, "%s: result contains %q", tc.name, str)
		}
	}
}
