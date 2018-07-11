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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

const (
	defaultSSHUser = "clusteroperator"

	// CIDR for service IPs. This is configured in Ansible via openshift_portal_net.
	defaultClusterServiceCIDR = "172.30.0.0/16"

	// CIDR for pod IPs. This is configured in Ansible via osm_cluster_network_cidr.
	defaultClusterPodCIDR = "10.128.0.0/14"

	// Domain for service names in the cluster. This is not configurable for OpenShift clusters.
	defaultServiceDomain = "svc.cluster.local"
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_ClusterDeploymentSpec(spec *ClusterDeploymentSpec) {
	if spec.Hardware.AWS != nil && spec.Hardware.AWS.SSHUser == "" {
		spec.Hardware.AWS.SSHUser = defaultSSHUser
	}

	if len(spec.NetworkConfig.ServiceDomain) == 0 {
		spec.NetworkConfig.ServiceDomain = defaultServiceDomain
	}

	if len(spec.NetworkConfig.Services.CIDRBlocks) == 0 {
		spec.NetworkConfig.Services = capiv1.NetworkRanges{CIDRBlocks: []string{defaultClusterServiceCIDR}}
	}
	if len(spec.NetworkConfig.Pods.CIDRBlocks) == 0 {
		spec.NetworkConfig.Pods = capiv1.NetworkRanges{CIDRBlocks: []string{defaultClusterPodCIDR}}
	}
}
