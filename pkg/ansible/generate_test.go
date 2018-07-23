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

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

const (
	imageFormat = "openshift/origin-${component}:v3.10.0"
)

func testClusterSpec(sshKeyPairName string) *coapi.ClusterDeploymentSpec {
	return &coapi.ClusterDeploymentSpec{
		Hardware: coapi.ClusterHardwareSpec{
			AWS: &coapi.AWSClusterSpec{
				Region:      "us-east-1",
				KeyPairName: sshKeyPairName,
				SSHUser:     "centos",
			},
		},
	}
}

func testClusterVersion() *coapi.OpenShiftConfigVersion {
	return &coapi.OpenShiftConfigVersion{
		Images: coapi.ClusterVersionImages{
			ImageFormat: imageFormat,
		},
		DeploymentType: coapi.ClusterDeploymentTypeOrigin,
		Version:        "v3.10.0",
	}
}

func TestGenerateClusterWideVars(t *testing.T) {
	tests := []struct {
		name             string
		clusterID        string
		clusterSpec      *coapi.ClusterDeploymentSpec
		infraSize        int
		clusterVersion   *coapi.OpenShiftConfigVersion
		sdnPluginName    string
		serviceCIDRs     capiv1.NetworkRanges
		podCIDRs         capiv1.NetworkRanges
		shouldInclude    []string
		shouldNotInclude []string
	}{
		{
			name:           "cluster",
			clusterID:      "testcluster",
			clusterSpec:    testClusterSpec("testcluster"),
			infraSize:      2,
			clusterVersion: testClusterVersion(),
			sdnPluginName:  "fakeplugin",
			serviceCIDRs:   capiv1.NetworkRanges{CIDRBlocks: []string{"172.30.0.0/16"}},
			podCIDRs:       capiv1.NetworkRanges{CIDRBlocks: []string{"10.128.0.0/14"}},
			shouldInclude: []string{
				"openshift_aws_clusterid: testcluster",
				"openshift_aws_vpc_name: testcluster",
				"openshift_aws_elb_master_external_name: testcluster-cp-ext",
				"openshift_aws_elb_master_internal_name: testcluster-cp-int",
				"openshift_aws_elb_infra_name: testcluster-infra",
				"openshift_aws_ssh_key_name: testcluster",
				"openshift_aws_region: us-east-1",
				"ansible_ssh_user: centos",
				"openshift_deployment_type: origin",
				"openshift_hosted_registry_replicas: 1",
				"openshift_portal_net: 172.30.0.0/16",
				"osm_cluster_network_cidr: 10.128.0.0/14",
				"os_sdn_network_plugin_name: \"fakeplugin\"",
				"openshift_aws_enable_uninstall_shared_objects: true",
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
			clusterSpec:    testClusterSpec("not_same_as_clusterid"),
			infraSize:      2,
			clusterVersion: testClusterVersion(),
			serviceCIDRs:   capiv1.NetworkRanges{CIDRBlocks: []string{"172.30.0.0/16"}},
			podCIDRs:       capiv1.NetworkRanges{CIDRBlocks: []string{"10.128.0.0/14"}},
			shouldInclude: []string{
				"openshift_aws_clusterid: 012345678901234567890123456789-abcde",
				"openshift_aws_elb_master_external_name: 0123456789012345678-abcde-cp-ext",
				"openshift_aws_elb_master_internal_name: 0123456789012345678-abcde-cp-int",
				"openshift_aws_elb_infra_name: 0123456789012345678-abcde-infra",
				"openshift_aws_ssh_key_name: not_same_as_clusterid",
				"openshift_aws_enable_uninstall_shared_objects: false",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenerateClusterWideVars(tc.clusterID, *tc.clusterSpec.Hardware.AWS, *tc.clusterVersion, tc.infraSize, tc.sdnPluginName, tc.serviceCIDRs, tc.podCIDRs)
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
