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

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	imageFormat = "openshift/origin-${component}:v3.10.0"
)

func testCluster() *coapi.Cluster {
	cluster := &coapi.Cluster{}
	cluster.Name = "testcluster"
	cluster.Spec.Hardware.AWS = &coapi.AWSClusterSpec{
		Region:      "us-east-1",
		KeyPairName: "mykey",
		SSHUser:     "centos",
	}
	return cluster
}

func testMachineSet() *coapi.MachineSet {
	cluster := testCluster()
	return &coapi.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testmachineset",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cluster, cluster.GroupVersionKind()),
			},
		},
		Spec: coapi.MachineSetSpec{
			MachineSetConfig: coapi.MachineSetConfig{
				NodeType: coapi.NodeTypeCompute,
				Infra:    false,
				Size:     3,
				Hardware: &coapi.MachineSetHardwareSpec{
					AWS: &coapi.MachineSetAWSHardwareSpec{
						InstanceType: "x9large",
					},
				},
			},
			ClusterHardware: testCluster().Spec.Hardware,
		},
	}
}

func testClusterMachineSetBuilder(name string, nodeType coapi.NodeType, infra bool, size int,
	cluster *coapi.Cluster) *coapi.ClusterMachineSet {
	return &coapi.ClusterMachineSet{
		ShortName: name,
		MachineSetConfig: coapi.MachineSetConfig{
			NodeType: nodeType,
			Infra:    infra,
			Size:     size,
			Hardware: &coapi.MachineSetHardwareSpec{
				AWS: &coapi.MachineSetAWSHardwareSpec{
					InstanceType: "x9large",
				},
			},
		},
	}
}

func testClusterWithInfra() *coapi.Cluster {
	cluster := testCluster()
	cluster.Spec.MachineSets = []coapi.ClusterMachineSet{
		*testClusterMachineSetBuilder("master", coapi.NodeTypeMaster, false, 3, cluster),
		*testClusterMachineSetBuilder("infra", coapi.NodeTypeCompute, true, 2, cluster),
	}
	return cluster
}

func testClusterVersion() *coapi.ClusterVersion {
	masterAMI := "master-AMI-west"
	return &coapi.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "origin-3-10",
		},
		Spec: coapi.ClusterVersionSpec{
			ImageFormat: imageFormat,
			VMImages: coapi.VMImages{
				AWSImages: &coapi.AWSVMImages{
					RegionAMIs: []coapi.AWSRegionAMIs{
						{
							Region: "us-east-1",
							AMI:    "compute-AMI-east",
						},
						{
							Region:    "us-west-1",
							AMI:       "compute-AMI-west",
							MasterAMI: &masterAMI,
						},
					},
				},
			},
			DeploymentType: coapi.ClusterDeploymentTypeOrigin,
			Version:        "v3.10.0",
		},
	}
}

func TestGenerateClusterVars(t *testing.T) {
	tests := []struct {
		name             string
		cluster          *coapi.Cluster
		clusterVersion   *coapi.ClusterVersion
		shouldInclude    []string
		shouldNotInclude []string
	}{
		{
			name:           "cluster",
			cluster:        testClusterWithInfra(),
			clusterVersion: testClusterVersion(),
			shouldInclude: []string{
				"openshift_aws_clusterid: testcluster",
				"openshift_aws_vpc_name: testcluster",
				"openshift_aws_elb_basename: testcluster",
				"openshift_aws_ssh_key_name: mykey",
				"openshift_aws_region: us-east-1",
				"ansible_ssh_user: centos",
				"openshift_deployment_type: origin",
			},
			shouldNotInclude: []string{
				"openshift_release",
				"openshift_pkg_version",
				"openshift_image_tag",
				"openshift_aws_base_ami",
				"openshift_aws_ami",
				"oreg_url",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenerateClusterVars(tc.cluster, &tc.clusterVersion.Spec)
			assert.Nil(t, err, "%s: unexpected: %v", tc.name, err)
			for _, str := range tc.shouldInclude {
				assert.Contains(t, result, str, "%s: result does not contain %q", tc.name, str)
			}
			for _, str := range tc.shouldNotInclude {
				assert.NotContains(t, result, str, "%s: result contains %q", tc.name, str)
			}
		})
	}
}

func TestLookupAMIForMachineSet(t *testing.T) {
	tests := []struct {
		name           string
		clusterVersion *coapi.ClusterVersion
		machineSet     *coapi.MachineSet
		expectedAMI    string // empty string for expected error
	}{
		{
			name:           "default AMI returned for compute node",
			clusterVersion: testClusterVersion(),
			machineSet:     testMachineSet(),
			expectedAMI:    "compute-AMI-east",
		},
		{
			name:           "default AMI returned for master node when no master AMI set",
			clusterVersion: testClusterVersion(),
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.NodeType = coapi.NodeTypeMaster
				return ms
			}(),
			expectedAMI: "compute-AMI-east",
		},
		{
			name:           "master AMI returned for master node",
			clusterVersion: testClusterVersion(),
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.NodeType = coapi.NodeTypeMaster
				ms.Spec.ClusterHardware.AWS.Region = "us-west-1"
				return ms
			}(),
			expectedAMI: "master-AMI-west",
		},
		{
			name:           "error on no AMI for region",
			clusterVersion: testClusterVersion(),
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.ClusterHardware.AWS.Region = "ca-central-1"
				return ms
			}(),
			expectedAMI: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			amiID, err := lookupAMIForMachineSet(tc.machineSet, tc.clusterVersion)
			assert.Equal(t, tc.expectedAMI, amiID)
			if tc.expectedAMI != "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestConvertVersionToRelease(t *testing.T) {
	tests := []struct {
		version         string
		expectedRelease string // empty string for expected error
	}{
		{
			version:         "v3.9.0-alpha.4+e7d9503-189",
			expectedRelease: "3.9",
		},
		{
			version:         "3.9.0-alpha.4+e7d9503-189",
			expectedRelease: "3.9",
		},
		{
			version:         "3.9.0",
			expectedRelease: "3.9",
		},
		{
			version:         "3.10",
			expectedRelease: "3.10",
		},
		{
			version:         "310",
			expectedRelease: "",
		},
	}
	for _, tc := range tests {
		t.Run("TestVersionForRelease"+tc.version, func(t *testing.T) {
			result, err := convertVersionToRelease(tc.version)
			assert.Equal(t, tc.expectedRelease, result)
			if tc.expectedRelease != "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestGenerateMachineSetVars(t *testing.T) {
	tests := []struct {
		name             string
		machineSet       *coapi.MachineSet
		shouldInclude    []string
		shouldNotInclude []string
	}{
		{
			name: "master machineset",
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.NodeType = coapi.NodeTypeMaster
				return ms
			}(),
			shouldInclude: []string{
				"master: compute-AMI-east",
				"openshift_aws_ami: compute-AMI-east",
				"instance_type: x9large",
				"desired_size: 3",
				"openshift_aws_clusterid: testcluster",
				"openshift_release: \"3.10\"",
				"oreg_url: " + imageFormat,
			},
		},
		{
			name: "infra machineset",
			machineSet: func() *coapi.MachineSet {
				ms := testMachineSet()
				ms.Spec.Infra = true
				ms.Spec.Size = 5
				return ms
			}(),
			shouldInclude: []string{
				"infra: compute-AMI-east",
				"openshift_aws_ami: compute-AMI-east",
				"instance_type: x9large",
				"desired_size: 5",
				"openshift_aws_clusterid: testcluster",
				"sub-host-type: infra",
				"openshift_release: \"3.10\"",
				"oreg_url: " + imageFormat,
			},
		},
		{
			name:       "compute machineset",
			machineSet: testMachineSet(),
			shouldInclude: []string{
				"compute: compute-AMI-east",
				"openshift_aws_ami: compute-AMI-east",
				"group: compute",
				"sub-host-type: compute",
				"instance_type: x9large",
				"desired_size: 3",
				"openshift_release: \"3.10\"",
				"oreg_url: " + imageFormat,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cv := testClusterVersion()
			result, err := GenerateMachineSetVars(tc.machineSet, cv)
			assert.Nil(t, err, "%s: unexpected: %v", tc.name, err)
			for _, str := range tc.shouldInclude {
				assert.Contains(t, result, str, "%s: result does not contain %q", tc.name, str)
			}
			for _, str := range tc.shouldNotInclude {
				assert.NotContains(t, result, str, "%s: result contains %q", tc.name, str)
			}
		})
	}
}
