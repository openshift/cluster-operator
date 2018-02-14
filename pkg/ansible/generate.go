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
	"text/template"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

// varsTemplate contains some hardcoded variables that are constant for all clusters,
// as well as the go template annotations to substitute in values we do need to change.
// Today this is AWS specific but will need to be broken down for multi-provider.
const (
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
openshift_deployment_type: origin

openshift_master_bootstrap_enabled: True

openshift_hosted_router_wait: False
openshift_hosted_registry_wait: False

################################################################################
# cluster specific settings
################################################################################

# Use containerized installation of master
containerized: True

# openshift_release must be specified.  Use whatever version of openshift
# that is supported by openshift-ansible that you wish.
# TODO: Parameterize
openshift_release: "3.7" # v3.7

# This will be dependent on the version provided by the yum repository
# TODO: Parameterize
openshift_pkg_version: -3.7.0 # -3.7.0

# TODO: development specific
openshift_disable_check: disk_availability,memory_availability,docker_storage,package_version,docker_image_availability
openshift_repos_enable_testing: true

# AWS region
# This value will instruct the plays where all items should be created.
# Multi-region deployments are not supported using these plays at this time.
openshift_aws_region: [[ .Region ]]

openshift_hosted_infra_selector: type=infra

#openshift_aws_create_launch_config: true
#openshift_aws_create_scale_group: true

# AWS_ACCESS_KEY_ID & AWS_SECRET_ACCESS_KEY are added to job environment
openshift_cloudprovider_kind: aws
openshift_cloudprovider_aws_access_key: "{{ lookup('env','AWS_ACCESS_KEY_ID') }}"
openshift_cloudprovider_aws_secret_key: "{{ lookup('env','AWS_SECRET_ACCESS_KEY') }}"

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
openshift_aws_base_ami: ami-ae7bfdb8

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
openshift_aws_iam_cert_chain_path: /ansible/ssl/ca.crt

openshift_aws_create_iam_role: True
openshift_node_use_instance_profiles: True

openshift_aws_clusterid: [[ .Name ]]
openshift_clusterid: [[ .Name ]]
openshift_aws_elb_basename: [[ .Name ]]
openshift_aws_vpc_name: [[ .Name ]]
`
	masterVarsTemplate = `
openshift_aws_ami_map:
  master: [[ .AMIName ]]

openshift_aws_master_group_config:
  # The 'master' key is always required here.
  master:
    instance_type: [[ .InstanceType ]]
    volumes: "{{ openshift_aws_node_group_config_master_volumes }}"
    health_check:
      period: 60
      type: EC2
    min_size: 1
    max_size: [[ .Size ]]
    desired_size: [[ .Size ]]
    wait_for_instances: True
    termination_policy: "{{ openshift_aws_node_group_termination_policy }}"
    replace_all_instances: "{{ openshift_aws_node_group_replace_all_instances }}"
    iam_role: "{{ openshift_aws_iam_role_name }}"
    policy_name: "{{ openshift_aws_iam_role_policy_name }}"
    policy_json: "{{ openshift_aws_iam_role_policy_json }}"
    elbs: "{{ openshift_aws_elb_dict | json_query('master.[*][0][*].name') }}"

openshift_aws_master_group:
- name: "{{ openshift_aws_clusterid }} master group"
  group: master
  tags:
    host-type: master
    sub-host-type: default
    runtime: docker
    Name: "{{ openshift_aws_clusterid }}-master"
`
	infraVarsTemplate = `
openshift_aws_ami_map:
  infra: [[ .AMIName ]]

openshift_aws_node_group_config:
  infra:
    instance_type: [[ .InstanceType ]]
    volumes: "{{ openshift_aws_node_group_config_node_volumes }}"
    health_check:
      period: 60
      type: EC2
    min_size: 1
    max_size: [[ .Size ]]
    desired_size: [[ .Size ]]
    termination_policy: "{{ openshift_aws_node_group_termination_policy }}"
    replace_all_instances: "{{ openshift_aws_node_group_replace_all_instances }}"
    iam_role: "{{ openshift_aws_iam_role_name }}"
    policy_name: "{{ openshift_aws_iam_role_policy_name }}"
    policy_json: "{{ openshift_aws_iam_role_policy_json }}"
    elbs: "{{ openshift_aws_elb_dict | json_query('infra.[*][0][*].name') }}"

openshift_aws_node_groups:
- name: "{{ openshift_aws_clusterid }} infra group"
  group: infra
  tags:
    host-type: node
    sub-host-type: infra
    runtime: docker
    Name: "{{ openshift_aws_clusterid }}-infra"
`

	computeVarsTemplate = `
openshift_aws_ami_map:
  compute: [[ .AMIName ]]

openshift_aws_node_groups:
- name: "{{ openshift_aws_clusterid }} compute group"
  group: compute
  tags:
    host-type: node
    sub-host-type: compute
    runtime: docker
    Name: "{{ openshift_aws_clusterid }}-compute"

openshift_aws_node_group_config:
  compute:
    instance_type: [[ .InstanceType ]]
    volumes: "{{ openshift_aws_node_group_config_node_volumes }}"
    health_check:
      period: 60
      type: EC2
    min_size: 1
    max_size: [[ .Size ]]
    desired_size: [[ .Size ]]
    termination_policy: "{{ openshift_aws_node_group_termination_policy }}"
    replace_all_instances: "{{ openshift_aws_node_group_replace_all_instances }}"
    iam_role: "{{ openshift_aws_iam_role_name }}"
    policy_name: "{{ openshift_aws_iam_role_policy_name }}"
    policy_json: "{{ openshift_aws_iam_role_policy_json }}"
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
)

type clusterParams struct {
	Name       string
	Region     string
	SSHKeyName string
	SSHUser    string
}

type machineSetParams struct {
	Name         string
	Size         int
	AMIName      string
	InstanceType string
}

// GenerateClusterVars generates the vars to pass to the ansible playbook
// for the cluster.
func GenerateClusterVars(cluster *coapi.Cluster) (string, error) {
	return GenerateClusterWideVars(cluster.Name, &cluster.Spec.Hardware)
}

// GenerateClusterWideVars generates the vars to pass to the ansible playbook
// that are set at the cluster level.
func GenerateClusterWideVars(name string, hardwareSpec *coapi.ClusterHardwareSpec) (string, error) {

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
		Name:       name,
		Region:     hardwareSpec.AWS.Region,
		SSHKeyName: hardwareSpec.AWS.KeyPairName,
		SSHUser:    hardwareSpec.AWS.SSHUser,
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// GenerateClusterWideVarsForMachineSet generates the vars to pass to the
// ansible playbook that are set at the cluster level for a machine set in
// that cluster.
func GenerateClusterWideVarsForMachineSet(machineSet *coapi.MachineSet) (string, error) {
	controllerRef := metav1.GetControllerOf(machineSet)
	if controllerRef == nil {
		return "", fmt.Errorf("machineset does not have a controller")
	}
	return GenerateClusterWideVars(controllerRef.Name, &machineSet.Spec.ClusterHardware)
}

// GenerateMachineSetVars generates the vars to pass to the ansible playbook
// for the machine set. The machine set must belong to the cluster.
func GenerateMachineSetVars(machineSet *coapi.MachineSet) (string, error) {
	commonVars, err := GenerateClusterWideVarsForMachineSet(machineSet)
	if err != nil {
		return "", err
	}

	if machineSet.Spec.Hardware == nil {
		return "", fmt.Errorf("no hardware for machine set")
	}
	if machineSet.Spec.Hardware.AWS == nil {
		return "", fmt.Errorf("no AWS spec found on machine set, only AWS is currently supported")
	}

	var buf bytes.Buffer
	buf.WriteString(commonVars)

	var varsTemplate string
	switch {
	case machineSet.Spec.NodeType == coapi.NodeTypeMaster:
		varsTemplate = masterVarsTemplate
	case machineSet.Spec.Infra:
		varsTemplate = infraVarsTemplate
	default:
		varsTemplate = computeVarsTemplate
	}

	t, err := template.New("machinesetvars").Delims("[[", "]]").Parse(varsTemplate)
	if err != nil {
		return "", err
	}

	params := &machineSetParams{
		Name:         machineSet.Name,
		Size:         machineSet.Spec.Size,
		InstanceType: machineSet.Spec.Hardware.AWS.InstanceType,
		AMIName:      machineSet.Spec.Hardware.AWS.AMIName,
	}

	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
