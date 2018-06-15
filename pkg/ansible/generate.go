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
	"bytes"
	"fmt"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"

	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
)

// varsTemplate contains some hardcoded variables that are constant for all clusters,
// as well as the go template annotations to substitute in values we do need to change.
// Today this is AWS specific but will need to be broken down for multi-provider.
const (
	vpcDefaults = `
  name: "{{ openshift_aws_vpc_name }}"
  cidr: 172.31.0.0/16
  subnets:
    us-east-1:
    - cidr: 172.31.48.0/20
      az: "us-east-1c"
      default_az: true
    - cidr: 172.31.32.0/20
      az: "us-east-1e"
    - cidr: 172.31.16.0/20
      az: "us-east-1a"
    us-east-2:
    - cidr: 172.31.48.0/20
      az: "us-east-2a"
      default_az: True
    - cidr: 172.31.32.0/20
      az: "us-east-2b"
      default_az: false
    - cidr: 172.31.16.0/20
      az: "us-east-2c"
    us-west-1:
    - cidr: 172.31.16.0/20
      az: "us-west-1c"
      default_az: True
    - cidr: 172.31.0.0/20
      az: "us-west-1b"
    us-west-2:
    - cidr: 172.31.48.0/20
      az: "us-west-2c"
      default_az: True
    - cidr: 172.31.32.0/20
      az: "us-west-2b"
    - cidr: 172.31.16.0/20
      az: "us-west-2a"
`
	clusterVarsTemplate = `---
# Variables that are commented in this file are optional; uncommented variables
# are mandatory.

# Default values for each variable are provided, as applicable.
# Example values for mandatory variables are provided as a comment at the end
# of the line.

# ------------------------ #
# Common/Cluster Variables #
# ------------------------ #
# Variables in this section affect all areas of the cluster
ansible_ssh_user: [[ .SSHUser ]]

################################################################################
# Ensure these variables are set for bootstrap
################################################################################
# Deployment type must be specified. ('origin' or 'openshift-enterprise')
openshift_deployment_type: [[ .DeploymentType ]]

openshift_master_bootstrap_enabled: True

openshift_hosted_router_wait: False
openshift_hosted_registry_wait: False
# cap to size 1 instead of .InfraSize while we use PV-backed registry storage
#openshift_hosted_registry_replicas: "{{ [ [[ .InfraSize ]], 2 ] | min }}"
openshift_hosted_registry_replicas: 1

# Override router edits to set the ROUTER_USE_PROXY_PROTOCOL
# environment variable. Defaults are taken from
# roles/openshift_hosted/defaults/main.yml in openshift-ansible and
# are set in addition to ROUTER_USE_PROXY_PROTOCOL.
openshift_hosted_router_edits:
- key: spec.strategy.rollingParams.intervalSeconds
  value: 1
  action: put
- key: spec.strategy.rollingParams.updatePeriodSeconds
  value: 1
  action: put
- key: spec.strategy.activeDeadlineSeconds
  value: 21600
  action: put
- key: spec.template.spec.containers[0].env
  value:
    name: ROUTER_USE_PROXY_PROTOCOL
    value: 'true'
  action: update

################################################################################
# cluster specific settings
################################################################################

# Use containerized installation of master
containerized: True

# TODO: development specific
openshift_disable_check: disk_availability,memory_availability,docker_storage,package_version,docker_image_availability

# AWS region
# This value will instruct the plays where all items should be created.
# Multi-region deployments are not supported using these plays at this time.
openshift_aws_region: [[ .Region ]]

openshift_hosted_infra_selector: "node-role.kubernetes.io/infra=true"

#openshift_aws_create_launch_config: true
#openshift_aws_create_scale_group: true

# AWS_ACCESS_KEY_ID & AWS_SECRET_ACCESS_KEY are added to job environment
openshift_cloudprovider_kind: aws
openshift_cloudprovider_aws_access_key: "{{ lookup('env','AWS_ACCESS_KEY_ID') }}"
openshift_cloudprovider_aws_secret_key: "{{ lookup('env','AWS_SECRET_ACCESS_KEY') }}"

# Enable auto-approve of CSRs
# TODO: Disable this when we have a controller that accepts nodes based on
# the existing cluster API machine resources
openshift_master_bootstrap_auto_approve: true

# --- #
# VPC #
# --- #

# openshift_aws_create_vpc defaults to true.  If you don't wish to provision
# a vpc, set this to false.
openshift_aws_create_vpc: true

# Name of the subnet in the vpc to use.  Needs to be set if using a pre-existing
# vpc + subnet.
#openshift_aws_subnet_name: cluster-engine-subnet-1

# -------------- #
# Security Group #
# -------------- #

# openshift_aws_create_security_groups defaults to true.  If you wish to use
# an existing security group, set this to false.
openshift_aws_create_security_groups: true

# openshift_aws_build_ami_group is the name of the security group to build the
# ami in.  This defaults to the value of openshift_aws_clusterid.
#openshift_aws_build_ami_group: cluster-engine

# openshift_aws_launch_config_security_groups specifies the security groups to
# apply to the launch config.  The launch config security groups will be what
# the cluster actually is deployed in.
# openshift_aws_launch_config_security_groups:
#   compute:
#   - cluster-engine
#   - cluster-engine_compute
#   - cluster-engine_compute_k8s
#   infra:
#   - cluster-engine
#   - cluster-engine_infra
#   - cluster-engine_infra_k8s
#   master:
#   - cluster-engine
#   - cluster-engine_master
#   - cluster-engine_master_k8s

# openshift_aws_node_security_groups are created when
# openshift_aws_create_security_groups is set to true.
#openshift_aws_node_security_groups: see roles/openshift_aws/defaults/main.yml

# -------- #
# ssh keys #
# -------- #

# Specify the key pair name here to connect to the provisioned instances.  This
# can be an existing key, or it can be one of the keys specified in
# openshift_aws_users
openshift_aws_ssh_key_name: [[ .SSHKeyName ]]

# This will ensure these user and public keys are created.
#openshift_aws_users:
#- key_name: myuser_key
#  username: myuser
#  pub_key: |
#         ssh-rsa AAAA

# -- #
# S3 #
# -- #

# Create an s3 bucket.
openshift_aws_create_s3: false

# --- #
# ELB #
# --- #

# openshift_aws_elb_name will be the base-name of the ELBs.
# TODO: looks like this is supposed to just be basename variant
#openshift_aws_elb_name: dgoodwin-dev

# custom certificates are required for the ELB
openshift_aws_iam_cert_path: /ansible/ssl/server.crt
openshift_aws_iam_cert_key_path: /ansible/ssl/server.key

# TODO: WARNING: set when we're not using self-signed certs
#openshift_aws_iam_cert_chain_path: /ansible/ssl/ca.crt

openshift_aws_create_iam_role: True
openshift_node_use_instance_profiles: True

openshift_aws_clusterid: [[ .ClusterID ]]
openshift_clusterid: [[ .ClusterID ]]
openshift_aws_elb_master_external_name: [[ .ELBMasterExternalName ]]
openshift_aws_elb_master_internal_name: [[ .ELBMasterInternalName ]]
openshift_aws_elb_infra_name: [[ .ELBInfraName ]]
openshift_aws_vpc_name: [[ .ClusterID ]]

openshift_aws_vpc: [[ .VPCDefaults ]]

[[if .ClusterAPIImage]]
cluster_api_image: [[ .ClusterAPIImage ]]
[[end]]
[[if .ClusterAPIImagePullPolicy]]
cluster_api_image_pull_policy: [[ .ClusterAPIImagePullPolicy ]]
[[end]]
[[if .MachineControllerImage]]
machine_controller_image: [[ .MachineControllerImage ]]
[[end]]
[[if .MachineControllerImagePullPolicy]]
machine_controller_image_pull_policy: [[ .MachineControllerImagePullPolicy ]]
[[end]]

openshift_aws_iam_master_role_name: "openshift_master_launch_instances"
openshift_aws_iam_master_role_policy_name: "launch_instances"
openshift_aws_iam_master_role_policy_json: "{{ lookup('template', 'launchinstances.json.j2') }}"

openshift_aws_master_group:
- name: "{{ openshift_aws_clusterid }} master group"
  group: master
  tags:
    host-type: master
    sub-host-type: default
    runtime: docker
    Name: "{{ openshift_aws_clusterid }}-master"

openshift_aws_iam_node_role_name: "openshift_node_describe_instances"
openshift_aws_iam_node_role_policy_json: "{{ lookup('file', 'describeinstances.json') }}"
openshift_aws_iam_node_role_policy_name: "describe_instances"

openshift_aws_node_groups:
- name: "{{ openshift_aws_clusterid }} infra group"
  group: infra
  tags:
    host-type: node
    sub-host-type: infra
    runtime: docker
    Name: "{{ openshift_aws_clusterid }}-infra"
- name: "{{ openshift_aws_clusterid }} compute group"
  group: compute
  tags:
    host-type: node
    sub-host-type: compute
    runtime: docker
    Name: "{{ openshift_aws_clusterid }}-compute"
`
	clusterVersionVarsTemplate = `

# ------- #
# Version #
# ------- #

openshift_release: "[[ .Release ]]"
oreg_url: [[ .ImageFormat ]]
openshift_aws_ami: [[ .AMI ]]
`
	DefaultInventory = `
[OSEv3:children]
masters
nodes
etcd

[OSEv3:vars]
ansible_become=true

[masters]

[etcd]

[nodes]
`
	masterMachinesInventory = `
[OSEv3:children]
masters
nodes
etcd

[OSEv3:vars]
ansible_become=true

[masters]
[[ range .MachineNames ]][[ . ]]
[[ end ]]
[etcd]
[[ range .MachineNames ]][[ . ]]
[[ end ]]
[nodes]
[[ range .MachineNames ]][[ . ]]
[[ end ]]
`
)

type clusterParams struct {
	ClusterID                        string
	Region                           string
	SSHKeyName                       string
	SSHUser                          string
	VPCDefaults                      string
	ELBMasterExternalName            string
	ELBMasterInternalName            string
	ELBInfraName                     string
	DeploymentType                   coapi.ClusterDeploymentType
	InfraSize                        int
	ClusterAPIImage                  string
	ClusterAPIImagePullPolicy        corev1.PullPolicy
	MachineControllerImage           string
	MachineControllerImagePullPolicy corev1.PullPolicy
}

type machineSetParams struct {
	Name         string
	Size         int
	AMI          string
	InstanceType string
}

type clusterVersionParams struct {
	Release     string
	AMI         string
	ImageFormat string
}

type inventoryParams struct {
	MachineNames []string
}

// GenerateClusterWideVars generates the vars to pass to the ansible playbook
// that are set at the cluster level.
func GenerateClusterWideVars(
	clusterID string,
	hardwareSpec *coapi.ClusterHardwareSpec,
	clusterVersion *coapi.ClusterVersion,
	infraSize int,
) (string, error) {

	// Currently only AWS is supported. If we don't have an AWS cluster spec, return an error
	if hardwareSpec.AWS == nil {
		return "", fmt.Errorf("no AWS spec found in the cluster, only AWS is currently supported")
	}

	// Change template delimiters to avoid conflict with {{ }} use in ansible vars:
	t, err := template.New("clustervars").Delims("[[", "]]").Parse(clusterVarsTemplate)
	if err != nil {
		return "", err
	}

	params := clusterParams{
		ClusterID:             clusterID,
		Region:                hardwareSpec.AWS.Region,
		SSHKeyName:            hardwareSpec.AWS.KeyPairName,
		SSHUser:               hardwareSpec.AWS.SSHUser,
		ELBMasterExternalName: controller.ELBMasterExternalName(clusterID),
		ELBMasterInternalName: controller.ELBMasterInternalName(clusterID),
		ELBInfraName:          controller.ELBInfraName(clusterID),
		VPCDefaults:           vpcDefaults,
		DeploymentType:        clusterVersion.Spec.DeploymentType,
		InfraSize:             infraSize,
	}

	if clusterVersion.Spec.ClusterAPIImage != nil {
		params.ClusterAPIImage = *clusterVersion.Spec.ClusterAPIImage
	}

	if clusterVersion.Spec.ClusterAPIImagePullPolicy != nil {
		params.ClusterAPIImagePullPolicy = *clusterVersion.Spec.ClusterAPIImagePullPolicy
	}

	if clusterVersion.Spec.MachineControllerImage != nil {
		params.MachineControllerImage = *clusterVersion.Spec.MachineControllerImage
	}

	if clusterVersion.Spec.MachineControllerImagePullPolicy != nil {
		params.MachineControllerImagePullPolicy = *clusterVersion.Spec.MachineControllerImagePullPolicy
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func lookupAMIForMachineSet(isMaster bool, clusterHardware *coapi.ClusterHardwareSpec, clusterVersion *coapi.ClusterVersion) (string, error) {
	for _, regionAMI := range clusterVersion.Spec.VMImages.AWSImages.RegionAMIs {
		if regionAMI.Region == clusterHardware.AWS.Region {
			if isMaster && regionAMI.MasterAMI != nil {
				return *regionAMI.MasterAMI, nil
			}
			return regionAMI.AMI, nil
		}
	}
	return "", fmt.Errorf("no AMI defined for cluster version %s/%s in region %v", clusterVersion.Namespace, clusterVersion.Name, clusterHardware.AWS.Region)
}

// convertVersionToRelease converts an OpenShift version string to it's major release. (i.e. 3.9.0 -> 3.9)
func convertVersionToRelease(version string) (string, error) {
	tokens := strings.Split(version, ".")
	if len(tokens) > 1 {
		release := tokens[0] + "." + tokens[1]
		if release[0] == 'v' {
			release = release[1:]
		}
		return release, nil
	}
	return "", fmt.Errorf("unable to parse release from version %v", version)
}

// GenerateClusterWideVarsForMachineSet generates the vars to pass to the
// ansible playbook that are set at the cluster level for a machine set in
// that cluster.
func GenerateClusterWideVarsForMachineSet(isMaster bool, clusterID string, clusterHardware *coapi.ClusterHardwareSpec, clusterVersion *coapi.ClusterVersion) (string, error) {
	// since we haven't been passed an infraSize, just assume minimum size of 1
	return GenerateClusterWideVarsForMachineSetWithInfraSize(isMaster, clusterID, clusterHardware, clusterVersion, 1)
}

// GenerateClusterWideVarsForMachineSetWithInfraSize generates the vars to pass to the
// ansible playbook that are set at the cluster level for a machine set in
// that cluster taking into account the size/count of infra nodes.
func GenerateClusterWideVarsForMachineSetWithInfraSize(
	isMaster bool,
	clusterID string,
	clusterHardware *coapi.ClusterHardwareSpec,
	clusterVersion *coapi.ClusterVersion,
	infraSize int,
) (string, error) {
	commonVars, err := GenerateClusterWideVars(clusterID, clusterHardware, clusterVersion, infraSize)

	// Layer in the vars that depend on the ClusterVersion:
	var buf bytes.Buffer
	buf.WriteString(commonVars)

	t, err := template.New("clusterversionvars").Delims("[[", "]]").Parse(clusterVersionVarsTemplate)
	if err != nil {
		return "", err
	}

	release, err := convertVersionToRelease(clusterVersion.Spec.Version)
	if err != nil {
		return "", err
	}

	amiID, err := lookupAMIForMachineSet(isMaster, clusterHardware, clusterVersion)
	if err != nil {
		return "", err
	}

	params := &clusterVersionParams{
		Release:     release,
		AMI:         amiID,
		ImageFormat: clusterVersion.Spec.ImageFormat,
	}

	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// GenerateInventoryForMasterMachines generates an inventory for the given master machines
func GenerateInventoryForMasterMachines(machines []*capi.Machine) (string, error) {
	t, err := template.New("inventory").Delims("[[", "]]").Parse(masterMachinesInventory)
	if err != nil {
		return "", err
	}

	machinePublicNames, err := getMachinePublicNames(machines)
	if err != nil {
		return "", err
	}

	params := &inventoryParams{
		MachineNames: machinePublicNames,
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func getMachinePublicNames(machines []*capi.Machine) ([]string, error) {
	result := []string{}
	for _, m := range machines {
		providerStatus, err := controller.AWSMachineProviderStatusFromClusterAPIMachine(m)
		if err != nil {
			return nil, err
		}
		if providerStatus == nil {
			return nil, fmt.Errorf("no AWS provider status in machine %s/%s", m.Namespace, m.Name)
		}
		if providerStatus.PublicDNS == nil {
			return nil, fmt.Errorf("no public DNS name is set for %s/%s", m.Namespace, m.Name)
		}
		result = append(result, *providerStatus.PublicDNS)
	}
	return result, nil
}
