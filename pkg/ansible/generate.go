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

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
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
all:
  vars:
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

    #
    # Registry configuration
    #
    [[ if .RegistryObjectStoreName ]]
    openshift_hosted_manage_registry: true
    openshift_hosted_registry_storage_kind: object
    openshift_hosted_registry_storage_provider: s3
    openshift_hosted_registry_storage_s3_accesskey: [[ .RegistryAccessKey ]]
    openshift_hosted_registry_storage_s3_secretkey: [[ .RegistrySecretKey ]]
    openshift_hosted_registry_storage_s3_region: [[ .Region ]]
    openshift_hosted_registry_storage_s3_bucket: [[ .RegistryObjectStoreName ]]
    openshift_docker_hosted_registry_insecure: False
    openshift_hosted_registry_selector: node-role.kubernetes.io/infra=true
    openshift_hosted_registry_storage_s3_encrypt: False
    openshift_hosted_registry_env_vars: {'REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXWAITINQUEUE': '2h', 'REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXRUNNING': '256'}
    openshift_hosted_registry_replicas: [[ .InfraSize ]]
    [[ else ]]
    # cap to size 1 instead of .InfraSize when using PV-backed registry storage
    openshift_hosted_registry_replicas: 1
    [[ end ]]

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
    
    os_sdn_network_plugin_name: "[[ .SDNPluginName ]]"
    openshift_portal_net: [[ .ServiceCIDR ]]
    osm_cluster_network_cidr: [[ .PodCIDR ]]
    
    # --- #
    # VPC #
    # --- #
    
    # openshift_aws_create_vpc defaults to true.  If you don't wish to provision
    # a vpc, set this to false.
    [[if .VPCName ]]
    openshift_aws_vpc_name: [[ .VPCName ]]
    openshift_aws_create_vpc: false
    [[else]]
    openshift_aws_vpc_name: [[ .ClusterID ]]
    openshift_aws_vpc: [[ .VPCDefaults ]]
    openshift_aws_create_vpc: true
    [[end]]
    
    # Name of the subnet in the vpc to use.  Needs to be set if using a pre-existing
    # vpc + subnet.
    #openshift_aws_subnet_name: cluster-engine-subnet-1
    [[if .SubnetName ]]
    openshift_aws_subnet_name: [[ .SubnetName ]]
    [[end]]
    
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
    openshift_aws_users:
    - key_name: [[ .SSHKeyName ]]
      pub_key: "{{ lookup('file', '/ansible/ssh/publickey.pub') }}"

    # This will remove the .SSHKeyName keypair from AWS
    openshift_aws_enable_uninstall_shared_objects: [[ .UninstallSSHKeyPair ]]
    
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
    
    [[if .ClusterAPIImage]]
    cluster_api_image: "[[ .ClusterAPIImage ]]"
    [[end]]
    [[if .ClusterAPIImagePullPolicy]]
    cluster_api_image_pull_policy: [[ .ClusterAPIImagePullPolicy ]]
    [[end]]
    [[if .MachineControllerImage]]
    machine_controller_image: "[[ .MachineControllerImage ]]"
    [[end]]
    [[if .MachineControllerImagePullPolicy]]
    machine_controller_image_pull_policy: [[ .MachineControllerImagePullPolicy ]]
    [[end]]
    
    openshift_aws_master_group:
    - name: "{{ openshift_aws_clusterid }} master group"
      group: master
      tags:
        host-type: master
        sub-host-type: default
        runtime: docker
        Name: "{{ openshift_aws_clusterid }}-master"
    
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
    oreg_url: "[[ .ImageFormat ]]"
    [[ if .EtcdImage ]]
    etcd_image: "[[ .EtcdImage ]]"
    [[ end ]]
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
	ClusterID                        string
	Region                           string
	SSHKeyName                       string
	SSHUser                          string
	UninstallSSHKeyPair              bool
	VPCDefaults                      string
	ELBMasterExternalName            string
	ELBMasterInternalName            string
	ELBInfraName                     string
	DeploymentType                   cov1.ClusterDeploymentType
	InfraSize                        int
	ClusterAPIImage                  string
	ClusterAPIImagePullPolicy        corev1.PullPolicy
	MachineControllerImage           string
	MachineControllerImagePullPolicy corev1.PullPolicy
	PodCIDR                          string
	ServiceCIDR                      string
	VPCName                          string
	SubnetName                       string
	SDNPluginName                    string
	RegistryObjectStoreName          string
	RegistryAccessKey                string
	RegistrySecretKey                string
}

type clusterVersionParams struct {
	Release     string
	ImageFormat string
	EtcdImage   string
}

// GenerateClusterWideVars generates the vars to pass to the ansible playbook
// that are set at the cluster level.
func GenerateClusterWideVars(
	clusterID string,
	hardwareSpec cov1.AWSClusterSpec,
	version cov1.OpenShiftConfigVersion,
	infraSize int,
	sdnPluginName string,
	serviceCIDRs capiv1.NetworkRanges,
	podCIDRs capiv1.NetworkRanges,
	registryAccessKey string,
	registrySecretKey string,
) (string, error) {

	// Change template delimiters to avoid conflict with {{ }} use in ansible vars:
	t, err := template.New("clustervars").Delims("[[", "]]").Parse(clusterVarsTemplate)
	if err != nil {
		return "", err
	}

	params := clusterParams{
		ClusterID:             clusterID,
		Region:                hardwareSpec.Region,
		SSHKeyName:            hardwareSpec.KeyPairName,
		SSHUser:               hardwareSpec.SSHUser,
		UninstallSSHKeyPair:   hardwareSpec.KeyPairName == clusterID, // only uninstall the cloud ssh keypair when it's unique to the cluster
		ELBMasterExternalName: cocontroller.ELBMasterExternalName(clusterID),
		ELBMasterInternalName: cocontroller.ELBMasterInternalName(clusterID),
		ELBInfraName:          cocontroller.ELBInfraName(clusterID),
		VPCDefaults:           vpcDefaults,
		DeploymentType:        version.DeploymentType,
		InfraSize:             infraSize,
		VPCName:               hardwareSpec.VPCName,
		SubnetName:            hardwareSpec.VPCSubnet,
		SDNPluginName:         sdnPluginName,
		// Openshift-ansible only supports a single value:
		ServiceCIDR: serviceCIDRs.CIDRBlocks[0],
		PodCIDR:     podCIDRs.CIDRBlocks[0],
	}

	if version.Images.ClusterAPIImage != nil {
		params.ClusterAPIImage = *version.Images.ClusterAPIImage
	}

	if version.Images.ClusterAPIImagePullPolicy != nil {
		params.ClusterAPIImagePullPolicy = *version.Images.ClusterAPIImagePullPolicy
	}

	if version.Images.MachineControllerImage != nil {
		params.MachineControllerImage = *version.Images.MachineControllerImage
	}

	if version.Images.MachineControllerImagePullPolicy != nil {
		params.MachineControllerImagePullPolicy = *version.Images.MachineControllerImagePullPolicy
	}

	if registryAccessKey != "" && registrySecretKey != "" {
		params.RegistryAccessKey = registryAccessKey
		params.RegistrySecretKey = registrySecretKey
		params.RegistryObjectStoreName = cocontroller.RegistryObjectStoreName(clusterID)
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
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
func GenerateClusterWideVarsForMachineSet(
	isMaster bool,
	clusterID string,
	clusterHardware cov1.AWSClusterSpec,
	clusterVersion cov1.OpenShiftConfigVersion,
	sdnPluginName string,
	serviceCIDRs capiv1.NetworkRanges,
	podCIDRs capiv1.NetworkRanges,
) (string, error) {
	// since we haven't been passed an infraSize, just assume minimum size of 1
	return GenerateClusterWideVarsForMachineSetWithInfraSize(isMaster, clusterID, clusterHardware, clusterVersion, 1, sdnPluginName, serviceCIDRs, podCIDRs, "" /* registry accessKey */, "" /* registry secretKey */)
}

// GenerateClusterWideVarsForMachineSetWithInfraSize generates the vars to pass to the
// ansible playbook that are set at the cluster level for a machine set in
// that cluster taking into account the size/count of infra nodes.
func GenerateClusterWideVarsForMachineSetWithInfraSize(
	isMaster bool,
	clusterID string,
	clusterHardware cov1.AWSClusterSpec,
	clusterVersion cov1.OpenShiftConfigVersion,
	infraSize int,
	sdnPluginName string,
	serviceCIDRs capiv1.NetworkRanges,
	podCIDRs capiv1.NetworkRanges,
	accessKey string,
	secretKey string,
) (string, error) {
	commonVars, err := GenerateClusterWideVars(clusterID, clusterHardware, clusterVersion, infraSize, sdnPluginName, serviceCIDRs, podCIDRs, accessKey, secretKey)

	// Layer in the vars that depend on the ClusterVersion:
	var buf bytes.Buffer
	buf.WriteString(commonVars)

	t, err := template.New("clusterversionvars").Delims("[[", "]]").Parse(clusterVersionVarsTemplate)
	if err != nil {
		return "", err
	}

	release, err := convertVersionToRelease(clusterVersion.Version)
	if err != nil {
		return "", err
	}

	params := &clusterVersionParams{
		Release:     release,
		ImageFormat: clusterVersion.Images.ImageFormat,
		EtcdImage:   clusterVersion.Images.EtcdImage,
	}

	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
