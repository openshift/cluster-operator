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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
)

const (
	imageFormat = "openshift/origin-${component}:v3.10.0"
)

func testClusterSpec() *coapi.ClusterDeploymentSpec {
	return &coapi.ClusterDeploymentSpec{
		Hardware: coapi.ClusterHardwareSpec{
			AWS: &coapi.AWSClusterSpec{
				Region:      "us-east-1",
				KeyPairName: "mykey",
				SSHUser:     "centos",
			},
		},
	}
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

func TestGenerateClusterWideVars(t *testing.T) {
	tests := []struct {
		name             string
		clusterID        string
		clusterSpec      *coapi.ClusterDeploymentSpec
		infraSize        int
		clusterVersion   *coapi.ClusterVersion
		shouldInclude    []string
		shouldNotInclude []string
	}{
		{
			name:           "cluster",
			clusterID:      "testcluster",
			clusterSpec:    testClusterSpec(),
			infraSize:      2,
			clusterVersion: testClusterVersion(),
			shouldInclude: []string{
				"openshift_aws_clusterid: testcluster",
				"openshift_aws_vpc_name: testcluster",
				"openshift_aws_elb_master_external_name: testcluster-master-external",
				"openshift_aws_elb_master_internal_name: testcluster-master-internal",
				"openshift_aws_elb_infra_name: testcluster-infra",
				"openshift_aws_ssh_key_name: mykey",
				"openshift_aws_region: us-east-1",
				"ansible_ssh_user: centos",
				"openshift_deployment_type: origin",
				"openshift_hosted_registry_replicas: 1",
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
		{
			name:           "long clusterID",
			clusterID:      "012345678901234567890123456789-abcde",
			clusterSpec:    testClusterSpec(),
			infraSize:      2,
			clusterVersion: testClusterVersion(),
			shouldInclude: []string{
				"openshift_aws_clusterid: 012345678901234567890123456789-abcde",
				"openshift_aws_elb_master_external_name: 0123456789-abcde-master-external",
				"openshift_aws_elb_master_internal_name: 0123456789-abcde-master-internal",
				"openshift_aws_elb_infra_name: 0123456789-abcde-infra",
				// TODO: Use these instead when the ansible playbook is updated to support it.
				// "openshift_aws_elb_master_external_name: 0123456789012345678-abcde-cp-ext",
				// "openshift_aws_elb_master_internal_name: 0123456789012345678-abcde-cp-int",
				// "openshift_aws_elb_infra_name: 0123456789012345678-abcde-infra",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenerateClusterWideVars(tc.clusterID, &tc.clusterSpec.Hardware, tc.clusterVersion, tc.infraSize)
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
		name            string
		isMaster        bool
		clusterVersion  *coapi.ClusterVersion
		clusterHardware *coapi.ClusterHardwareSpec
		expectedAMI     string // empty string for expected error
	}{
		{
			name:            "default AMI returned for compute node",
			isMaster:        false,
			clusterVersion:  testClusterVersion(),
			clusterHardware: &testClusterSpec().Hardware,
			expectedAMI:     "compute-AMI-east",
		},
		{
			name:            "default AMI returned for master node when no master AMI set",
			isMaster:        true,
			clusterVersion:  testClusterVersion(),
			clusterHardware: &testClusterSpec().Hardware,
			expectedAMI:     "compute-AMI-east",
		},
		{
			name:           "master AMI returned for master node",
			isMaster:       true,
			clusterVersion: testClusterVersion(),
			clusterHardware: func() *coapi.ClusterHardwareSpec {
				h := &testClusterSpec().Hardware
				h.AWS.Region = "us-west-1"
				return h
			}(),
			expectedAMI: "master-AMI-west",
		},
		{
			name:           "error on no AMI for region",
			isMaster:       false,
			clusterVersion: testClusterVersion(),
			clusterHardware: func() *coapi.ClusterHardwareSpec {
				h := &testClusterSpec().Hardware
				h.AWS.Region = "ca-central-1"
				return h
			}(),
			expectedAMI: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			amiID, err := lookupAMIForMachineSet(tc.isMaster, tc.clusterHardware, tc.clusterVersion)
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

func TestGenerateInventoryForMasterMachines(t *testing.T) {
	count := 0
	machine := func(publicName string) *capi.Machine {
		count++
		m := &capi.Machine{}
		m.Name = fmt.Sprintf("count-%d", count)
		m.Namespace = "default"

		awsStatus := &coapi.AWSMachineProviderStatus{
			PublicDNS: &publicName,
		}
		providerStatus, err := controller.ClusterAPIMachineProviderStatusFromAWSMachineProviderStatus(awsStatus)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m.Status.ProviderStatus = providerStatus
		return m
	}
	machines := []*capi.Machine{
		machine("first.public.ip"),
		machine("second.public.ip"),
		machine("third.public.ip"),
	}
	actual, err := GenerateInventoryForMasterMachines(machines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `
[OSEv3:children]
masters
nodes
etcd

[OSEv3:vars]
ansible_become=true

[masters]
first.public.ip
second.public.ip
third.public.ip

[etcd]
first.public.ip
second.public.ip
third.public.ip

[nodes]
first.public.ip
second.public.ip
third.public.ip
`
	if strings.TrimSpace(actual) != strings.TrimSpace(expected) {
		t.Errorf("unexpected result:\n%s", actual)
	}
}
