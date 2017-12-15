package ansible

import (
	"bytes"
	"fmt"
	"text/template"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

// varsTemplate contains some hardcoded variables that are constant for all clusters,
// as well as the go template annotations to substitute in values we do need to change.
// Today this is AWS specific but will need to be broken down for multi-provider.
const varsTemplate = `---
# Variables that are commented in this file are optional; uncommented variables
# are mandatory.

# Default values for each variable are provided, as applicable.
# Example values for mandatory variables are provided as a comment at the end
# of the line.

# ------------------------ #
# Common/Cluster Variables #
# ------------------------ #
# Variables in this section affect all areas of the cluster
# TODO:
ansible_ssh_user: centos

################################################################################
# Ensure these variables are set for bootstrap
################################################################################
# Deployment type must be specified. ('origin' or 'openshift-enterprise')
openshift_deployment_type: openshift-enterprise

openshift_master_bootstrap_enabled: True

openshift_hosted_router_wait: False
openshift_hosted_registry_wait: False

openshift_master_api_port: 443
openshift_master_console_port: 443

################################################################################
# cluster specific settings
################################################################################

# openshift_release must be specified.  Use whatever version of openshift
# that is supported by openshift-ansible that you wish.
openshift_release: "v3.7" # v3.7

# This will be dependent on the version provided by the yum repository
openshift_pkg_version: -3.7.0 # -3.7.0

# TODO: development specific
openshift_disable_check: disk_availability,memory_availability,docker_storage,package_version,docker_image_availability
openshift_repos_enable_testing: true

# AWS region
# This value will instruct the plays where all items should be created.
# Multi-region deployments are not supported using these plays at this time.


#openshift_aws_create_launch_config: true
#openshift_aws_create_scale_group: true

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
openshift_aws_launch_config_security_groups:
  compute:
  - cluster-engine
  - cluster-engine_compute
  - cluster-engine_compute_k8s
  infra:
  - cluster-engine
  - cluster-engine_infra
  - cluster-engine_infra_k8s
  master:
  - cluster-engine
  - cluster-engine_master
  - cluster-engine_master_k8s

# openshift_aws_node_security_groups are created when
# openshift_aws_create_security_groups is set to true.
#openshift_aws_node_security_groups: see roles/openshift_aws/defaults/main.yml

# -------- #
# ssh keys #
# -------- #

# Specify the key pair name here to connect to the provisioned instances.  This
# can be an existing key, or it can be one of the keys specified in
# openshift_aws_users
openshift_aws_ssh_key_name: libra # myuser_key

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
openshift_aws_iam_cert_path: /home/abutcher/rhat/certs/wildcard-flibberty-jibbet.com.crt  # '/path/to/wildcard.<clusterid>.example.com.crt'
openshift_aws_iam_cert_key_path: /home/abutcher/rhat/certs/wildcard-flibberty-jibbet.com.key  # '/path/to/wildcard.<clusterid>.example.com.key'
openshift_aws_iam_cert_chain_path: /home/abutcher/rhat/certs/ca.crt  # '/path/to/cert.ca.crt'

openshift_aws_create_iam_role: True
openshift_node_use_instance_profiles: True

[[ range $key, $value := . ]]
[[ $key ]]: [[ $value ]]
[[ end ]]
`

// TODO: these {{}} don't play nice in a go template, complete this when we get to processing
// node groups dynamically
/*
# tags
openshift_aws_master_group_tags:
  Name: "{{ openshift_aws_clusterid }}-master"
  host-type: master
  sub-host-type: default

openshift_aws_node_group_compute_tags:
  Name: "{{ openshift_aws_clusterid }}-compute"
  host-type: node
  sub-host-type: compute

openshift_aws_node_group_infra_tags:
  Name: "{{ openshift_aws_clusterid }}-infra"
  host-type: node
  sub-host-type: infra
*/

const (
	cloudProviderKindVar = "openshift_cloudprovider_kind"
	awsClusterIDVar      = "openshift_aws_clusterid"
	elbBasenameVar       = "openshift_aws_elb_basename"
	awsVPCNameVar        = "openshift_aws_vpc_name"
	awsRegionVar         = "openshift_aws_region"
	awsAMIVar            = "openshift_aws_ami"
)

type VarsGenerator struct {
	Vars map[string]string
}

func NewVarsGenerator(cluster *coapi.Cluster) *VarsGenerator {

	// Any vars that change from cluster to cluster need to be defined here:
	inv := &VarsGenerator{
		Vars: map[string]string{
			cloudProviderKindVar: "aws",
			awsRegionVar:         "us-east-1",
			awsClusterIDVar:      cluster.Name,
			elbBasenameVar:       cluster.Name,
			awsVPCNameVar:        cluster.Name,
			awsAMIVar:            fmt.Sprintf("%s-ami-base", cluster.Name),
		},
	}
	return inv
}

// GenerateVars returns a string suitable for use as a vars file when calling openshift-ansible pods.
func (ig *VarsGenerator) GenerateVars() (string, error) {
	// Change template delimiters to avoid conflict with {{ }} use in ansible vars:
	t, err := template.New("varsfilecontents").Delims("[[", "]]").Parse(varsTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, ig.Vars)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
