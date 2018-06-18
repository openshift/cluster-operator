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

package clusteroperator

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Annotation constants
const (
	// OwnerGenerationAnnotation contains the generation of the spec of the
	// owner of a job
	OwnerGenerationAnnotation = GroupName + "/owner.generation"

	// ClusterNameLabel is the label that a machineset must have to identify the
	// cluster to which it belongs.
	ClusterNameLabel = "clusteroperator.openshift.io/cluster"

	// ClusterDeploymentLabel is the label used on clusters to link to the ClusterDeployment that sourced it.
	ClusterDeploymentLabel = "clusteroperator.openshift.io/cluster-deployment"

	// MasterMachineSetName is the short name reserved for master machine sets.
	// This cannot be used as the short name for a regular compute machine set.
	MasterMachineSetName = "master"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterDeployment represents a deployment of a cluster that clusteroperator
// manages
type ClusterDeployment struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// +optional
	Spec ClusterDeploymentSpec
	// +optional
	Status ClusterDeploymentStatus
}

// finalizer values unique to cluster-operator
const (
	FinalizerClusterDeployment string = "clusteroperator.openshift.io/clusterdeployment"
	FinalizerRemoteMachineSets string = "clusteroperator.openshift.io/remotemachinesets"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterDeploymentList is a list of ClusterDeployments.
type ClusterDeploymentList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []ClusterDeployment
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterVersion represents a version of OpenShift that can be, or is installed.
type ClusterVersion struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// +optional
	Spec ClusterVersionSpec
	// +optional
	Status ClusterVersionStatus
}

// ClusterVersionSpec is a specification of a cluster version that can be installed.
type ClusterVersionSpec struct {
	// ImageFormat defines a format string for the container registry and images to use for
	// various OpenShift components. Valid expansions are component (required, expands to
	// pod/deployer/haproxy-router/etc), and version (v3.9.0).
	// (i.e. example.com/openshift3/ose-${component}:${version}")
	ImageFormat string

	VMImages VMImages

	// DeploymentType indicates the type of OpenShift deployment to create.
	DeploymentType ClusterDeploymentType

	// Version is the version of OpenShift to install.
	Version string

	// OpenshiftAnsibleImage is the name of the image to use to run
	// openshift-ansible playbooks.
	// Defaults to openshift/origin-ansbile:{TAG}, where {TAG} is
	// the value from the Version field of this ClusterVersion.
	// +optional
	OpenshiftAnsibleImage *string

	// OpenshiftAnsibleImagePullPolicy is the pull policy to use for
	// OpenshiftAnsibleImage.
	// Defaults to IfNotPreset.
	// +optional
	OpenshiftAnsibleImagePullPolicy *corev1.PullPolicy

	// ClusterAPIImage is the name of the image to use on the target
	// cluster to run Cluster API
	// +optional
	ClusterAPIImage *string

	// ClusterAPIImagePullPolicy is the pull policy to use for the
	// ClusterAPIImage
	// +optional
	ClusterAPIImagePullPolicy *corev1.PullPolicy

	// MachineControllerImage is the name of the image to use on the
	// target cluster to run the machine controller
	// +optional
	MachineControllerImage *string

	// MachineControllerImagePullPolicy is the pull policy to use for
	// the MachineControllerImagePullPolicy
	// +optional
	MachineControllerImagePullPolicy *corev1.PullPolicy
}

// ClusterVersionStatus is the status of a ClusterVersion. It may be used to indicate if the
// cluster version is ready to be used, or if any problems have been detected.
type ClusterVersionStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterVersionList is a list of ClusterVersions.
type ClusterVersionList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []ClusterVersion
}

// VMImages contains required data to locate images in all supported cloud providers.
type VMImages struct {
	// +optional
	AWSImages *AWSVMImages
	// TODO: GCP, Azure added as necessary.
}

// AWSVMImages indicates which AMI to use in each supported AWS region for this OpenShift version.
type AWSVMImages struct {
	RegionAMIs []AWSRegionAMIs
}

// AWSRegionAMIs defines which AMI to use for a node type in a given region.
type AWSRegionAMIs struct {
	Region string
	// AMI is the ID of the AMI to use for compute nodes in the cluster. If no MasterAMI is defined, it will be used for masters as well.
	AMI string
	// MasterAMI is the ID of the AMI to use for the master nodes in the cluster. If unset, the default AMI will be used instead.
	// +optional
	MasterAMI *string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterProviderConfigSpec is the cluster specification stored in the
// ProviderConfig of a cluster.k8s.io Cluster.
type ClusterProviderConfigSpec struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	ClusterDeploymentSpec
}

// ClusterDeploymentSpec is the specification of a cluster's hardware and configuration
type ClusterDeploymentSpec struct {
	// ClusterID is the ID to assign to the cluster in the cloud provider.
	// This will be generated by the API Server when the ClusterDeployment is
	// created. It should not be set by the user.
	ClusterID string

	// Hardware specifies the hardware that the cluster will run on
	Hardware ClusterHardwareSpec

	// Config specifies cluster-wide OpenShift configuration
	Config ClusterConfigSpec

	// DefaultHardwareSpec specifies hardware defaults for all machine sets
	// in this cluster
	// +optional
	DefaultHardwareSpec *MachineSetHardwareSpec

	// MachineSets specifies the configuration of all machine sets for the cluster
	MachineSets []ClusterMachineSet

	// ClusterVersionRef references the clusterversion that should be running.
	ClusterVersionRef ClusterVersionReference
}

// ClusterVersionReference provides information to locate a cluster version to use.
type ClusterVersionReference struct {
	// Namespace of the clusterversion.
	// +optional
	Namespace string

	// Name of the clusterversion.
	Name string
}

// ClusterHardwareSpec specifies hardware for a cluster. The specification will
// be specific to each cloud provider.
type ClusterHardwareSpec struct {
	// AWS specifies cluster hardware configuration on AWS
	// +optional
	AWS *AWSClusterSpec

	// TODO: Add other cloud-specific Specs as needed
}

// AWSClusterSpec contains cluster-wide configuration for a cluster on AWS
type AWSClusterSpec struct {
	// AccountSeceret refers to a secret that contains the AWS account access
	// credentials
	AccountSecret corev1.LocalObjectReference

	// SSHSecret refers to a secret that contains the ssh private key to access
	// EC2 instances in this cluster.
	SSHSecret corev1.LocalObjectReference

	// SSHUser refers to the username that should be used for Ansible to SSH to the system. (default: clusteroperator)
	// +optional
	SSHUser string

	// SSLSecret refers to a secret that contains the SSL certificate to use
	// for this cluster. The secret is expected to contain the following keys:
	// - ca.crt - the certificate authority certificate (optional)
	// - server.crt - the server certificate
	// - server.key - the server key
	SSLSecret corev1.LocalObjectReference

	// KeyPairName is the name of the AWS key pair to use for SSH access to EC2
	// instances in this cluster
	KeyPairName string

	// Region specifies the AWS region where the cluster will be created
	Region string

	// VPCName specifies the name of the VPC to associate with the cluster.
	// If a value is specified, a VPC will be created with that name if it
	// does not already exist in the cloud provider. If it does exist, the
	// existing VPC will be used.
	// If no name is specified, a VPC name will be generated using the
	// cluster name and created in the cloud provider.
	// +optional
	VPCName string

	// VPCSubnet specifies the subnet to use for the cluster's VPC. Only used
	// when a new VPC is created for the cluster
	// +optional
	VPCSubnet string
}

// ClusterConfigSpec contains OpenShift configuration for a cluster
type ClusterConfigSpec struct {
	// SDNPluginName is the name of the SDN plugin to use for this install
	SDNPluginName string

	// ServiceNetworkSubnet is the CIDR to use for service IPs in the cluster
	// +optional
	ServiceNetworkSubnet string

	// PodNetworkSubnet is the CIDR to use for pod IPs in the cluster
	// +optional
	PodNetworkSubnet string
}

// ClusterDeploymentType is a valid value for ClusterConfigSpec.DeploymentType
type ClusterDeploymentType string

// These are valid values for cluster deployment type
const (
	// ClusterDeploymentTypeOrigin is a deployment type of origin
	ClusterDeploymentTypeOrigin ClusterDeploymentType = "origin"
	// ClusterDeploymentTypeEnterprise is a deployment type of openshift enterprise
	ClusterDeploymentTypeEnterprise ClusterDeploymentType = "openshift-enterprise"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterProviderStatus is the cluster status stored in the
// ProviderStatus of a cluster.k8s.io Cluster.
type ClusterProviderStatus struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	ClusterDeploymentStatus
}

// ClusterDeploymentStatus contains the status for a cluster
type ClusterDeploymentStatus struct {
	// MachineSetCount is the number of actual machine sets that are active for the cluster
	MachineSetCount int

	// MasterMachineSetName is the name of the master machine set
	MasterMachineSetName string

	// InfraMachineSetName is the name of the infra machine set
	InfraMachineSetName string

	// AdminKubeconfig points to a secret containing a cluster administrator
	// kubeconfig to access the cluster. The secret can be used for bootstrapping
	// and subsequent access to the cluster API.
	// +optional
	AdminKubeconfig *corev1.LocalObjectReference

	// Provisioned is true if the hardware pre-reqs for the cluster have been provisioned
	// For machine set hardware, see the status of each machine set resource.
	Provisioned bool

	// ProvisionedJobGeneration is the generation of the cluster resource used to
	// generate the latest completed infra provisioning job.
	ProvisionedJobGeneration int64

	// ControlPlaneInstalled is true if the control plane is installed
	// and running in the cluster.
	ControlPlaneInstalled bool

	// ControlPlaneInstalledJobClusterGeneration is the generation of the cluster
	// resource used to generate the latest completed control-plane installation
	// job.
	ControlPlaneInstalledJobClusterGeneration int64

	// ControlPlaneInstalledJobMachineSetGeneration is the generation of the
	// master machine set resource used to generate the latest completed
	// control-plane installation job.
	ControlPlaneInstalledJobMachineSetGeneration int64

	// ComponentsInstalled is true if the additional components needed for the
	// cluster have been installed
	ComponentsInstalled bool

	// ComponentsInstalledJobGeneration is the generation of the cluster resource
	// used to generate the latest completed components installation job.
	ComponentsInstalledJobClusterGeneration int64

	// ComponentsInstalledJobMachineSetGeneration is the generation of the
	// master machine set resource used to generate the latest completed
	// components installation job.
	ComponentsInstalledJobMachineSetGeneration int64

	// NodeConfigInstalled is true if the node config daemonset has been created
	// in the cluster.
	NodeConfigInstalled bool

	// NodeConfigInstalledJobClusterGeneration is the generation of the cluster
	// resource used to generate the latest successful node-config installation job.
	NodeConfigInstalledJobClusterGeneration int64

	// NodeConfigInstalledJobMachineSetGeneration is the generation of the
	// master machine set resource used to generate the latest successful
	// node-config installation job.
	NodeConfigInstalledJobMachineSetGeneration int64

	// NodeConfigInstalledTime is the time of the last successful installation of
	// the node config daemonset in the cluster.
	NodeConfigInstalledTime *metav1.Time

	// ClusterAPIInstalled is true if the Kubernetes Cluster API controllers have
	// been deployed onto the remote cluster.
	ClusterAPIInstalled bool

	// ClusterAPIInstalledJobClusterGeneration is the generation of the cluster
	// resource used to generate the latest completed deployclusterapi
	// installation job.
	ClusterAPIInstalledJobClusterGeneration int64

	// ClusterAPIInstalledJobMachineSetGeneration is the generation of the machine
	// set resource used to generate the latest completed deployclusterapi
	// installation job.
	ClusterAPIInstalledJobMachineSetGeneration int64

	// ClusterAPIInstalledTime is the time we last successfully deployed the
	// Kubernetes Cluster API controllers on the cluster.
	ClusterAPIInstalledTime *metav1.Time

	// Ready is true if the master of the cluster is ready to be used and can be accessed using
	// the KubeconfigSecret
	Ready bool

	// Conditions includes more detailed status for the cluster
	Conditions []ClusterCondition

	// ClusterVersionRef references the resolved clusterversion the cluster should be running.
	// +optional
	ClusterVersionRef *corev1.ObjectReference
}

// ClusterCondition contains details for the current condition of a cluster
type ClusterCondition struct {
	// Type is the type of the condition.
	Type ClusterConditionType
	// Status is the status of the condition.
	Status corev1.ConditionStatus
	// LastProbeTime is the last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time
	// LastTransitionTime is the last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time
	// Reason is a unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string
	// Message is a human-readable message indicating details about last transition.
	// +optional
	Message string
}

// ClusterConditionType is a valid value for ClusterCondition.Type
type ClusterConditionType string

// These are valid conditions for a cluster
const (
	// ClusterInfraProvisionining is true when cluster infrastructure is in the process of provisioning
	ClusterInfraProvisioning ClusterConditionType = "InfraProvisioning"
	// ClusterInfraProvisioningFailed is true when the job to provision cluster infrastructure has failed.
	ClusterInfraProvisioningFailed ClusterConditionType = "InfraProvisioningFailed"
	// ClusterInfraProvisioned represents success of the infra provisioning for this cluster.
	ClusterInfraProvisioned ClusterConditionType = "InfraProvisioned"
	// ClusterInfraDeprovisioning is true when cluster infrastructure is in the process of deprovisioning
	ClusterInfraDeprovisioning ClusterConditionType = "InfraDeprovisioning"
	// ClusterInfraDeprovisioningFailed is true when the job to deprovision cluster infrastructure has failed.
	ClusterInfraDeprovisioningFailed ClusterConditionType = "InfraDeprovisioningFailed"
	// ControlPlaneInstalling is true if the control plane is being installed in
	// the cluster.
	ControlPlaneInstalling ClusterConditionType = "ControlPlaneInstalling"
	// ControlPlaneInstallationFailed is true if the installation of
	// the control plane failed.
	ControlPlaneInstallationFailed ClusterConditionType = "ControlPlaneInstallationFailed"
	// ControlPlaneInstalled is true if the control plane has been installed
	// in the cluster.
	ControlPlaneInstalled ClusterConditionType = "ControlPlaneInstalled"
	// ComponentsInstalling is true if the OpenShift extra components are being
	// installed in the cluster.
	ComponentsInstalling ClusterConditionType = "ComponentsInstalling"
	// ComponentsInstallationFailed is true if the installation of
	// the OpenShift extra components failed.
	ComponentsInstallationFailed ClusterConditionType = "ComponentsInstallationFailed"
	// ControlPlaneInstalled is true if the OpenShift extra components have been
	// installed in the cluster.
	ComponentsInstalled ClusterConditionType = "ComponentsInstalled"
	// NodeConfigInstalling is true if node config daemonset is being installed.
	NodeConfigInstalling ClusterConditionType = "NodeConfigInstalling"
	// NodeConfigInstallationFailed is true if the node config daemonset failed to be installed.
	NodeConfigInstallationFailed ClusterConditionType = "NodeConfigInstallationFailed"
	// NodeConfigInstalled is true if the node config daemonset has been installed.
	NodeConfigInstalled ClusterConditionType = "NodeConfigInstalled"
	// ClusterAPIInstalling is true if the Kubernetes Cluster API controllers are
	// being installed on the remote cluster.
	ClusterAPIInstalling ClusterConditionType = "ClusterAPIInstalling"
	// ClusterAPIInstallationFailed is true if the installation of the Kubernetes
	// Cluster API controllers failed.
	ClusterAPIInstallationFailed ClusterConditionType = "ClusterAPIInstallationFailed"
	// ClusterAPIInstalled is true if the Kubernetes Cluster API controllers have
	// been successfully installed.
	ClusterAPIInstalled ClusterConditionType = "ClusterAPIInstalled"
	// ClusterVersionIncompatible is true when the cluster version does not have an AMI defined for the cluster's region.
	ClusterVersionIncompatible ClusterConditionType = "VersionIncompatible"
	// ClusterVersionMissing is true when the cluster references a non-existent cluster version
	ClusterVersionMissing ClusterConditionType = "ClusterVersionMissing"
	// ClusterReady means the cluster is able to service requests
	ClusterReady ClusterConditionType = "Ready"
)

// ClusterMachineSet is the specification of a machine set in a cluster
type ClusterMachineSet struct {
	// ShortName is a unique name for the machine set within the cluster
	ShortName string

	// MachineSetConfig is the configuration for the MachineSet
	MachineSetConfig
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineSetProviderConfigSpec is the machine set specification stored in the
// ProviderConfig of a cluster.k8s.io MachineSpec.
type MachineSetProviderConfigSpec struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	MachineSetSpec
}

// MachineSetConfig contains configuration for a MachineSet
type MachineSetConfig struct {
	// NodeType is the type of nodes that comprise the MachineSet
	// TODO: remove in favor of upstream MachineTemplateSpec roles.
	NodeType NodeType

	// Infra indicates whether this machine set should contain infrastructure
	// pods
	// TODO: remove in favor of upstream MachineTemplateSpec roles.
	Infra bool

	// Size is the number of nodes that the node group should contain
	// TODO: remove in favor of upstream MachineSet and MachineDeployment replicas.
	Size int

	// Hardware defines what the hardware should look like for this
	// MachineSet. The specification will vary based on the cloud provider.
	// +optional
	Hardware *MachineSetHardwareSpec

	// NodeLabels specifies the labels that will be applied to nodes in this
	// MachineSet
	NodeLabels map[string]string
}

// MachineSetSpec is the Cluster Operator specification for a Cluster API machine template provider config.
// TODO: This should be renamed, it is now used on MachineTemplate.Spec.ProviderConfig.
type MachineSetSpec struct {
	// ClusterID is the ID of the cluster in the cloud provider.
	ClusterID string

	// MachineSetConfig is the configuration for the MachineSet
	MachineSetConfig

	// ClusterHardware specifies the hardware that the cluster will run on. It is typically a copy of
	// the clusters data and set automatically by controllers.
	ClusterHardware ClusterHardwareSpec

	// ClusterVersionRef references the clusterversion the machine set is running.
	ClusterVersionRef corev1.ObjectReference

	// VMImage contains a single cloud provider specific image to use for this machine set.
	VMImage VMImage
}

// VMImage contains a specified single image to use for a supported cloud provider.
type VMImage struct {
	// +optional
	AWSImage *string
}

// MachineSetHardwareSpec specifies the hardware for a MachineSet
type MachineSetHardwareSpec struct {
	// AWS specifies the hardware spec for an AWS machine set
	// +optional
	AWS *MachineSetAWSHardwareSpec
}

// MachineSetAWSHardwareSpec specifies AWS hardware for a MachineSet
type MachineSetAWSHardwareSpec struct {
	// InstanceType is the type of instance to use for machines in this MachineSet
	// +optional
	InstanceType string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSMachineProviderStatus is the AWS specific provider status for a cluster.k8s.io Machine.
type AWSMachineProviderStatus struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// InstanceID is the AWS instance ID for this machine.
	// +optional
	InstanceID *string

	// InstanceState is the state of the AWS instance for this machine.
	InstanceState *string

	// PublicIP is the public IP address for this machine.
	// +optional
	PublicIP *string

	// PrivateIP is the internal IP address for this machine.
	// +optional
	PrivateIP *string

	// PublicDNS is the public DNS hostname for this machine.
	// +optional
	PublicDNS *string

	// PrivateDNS is the internal DNS hostname for this machine.
	// +optional
	PrivateDNS *string

	// LastELBSync stores when we last successfully ensured a master machine is added to relevant load balancers.
	// +optional
	LastELBSync *metav1.Time

	// LastELBSyncGeneration is the generation of the machine resource last added to the ELB.
	LastELBSyncGeneration int64
}

// NodeType is the type of the Node
type NodeType string

const (
	// NodeTypeMaster is a node that is a master in the cluster
	NodeTypeMaster NodeType = "Master"
	// NodeTypeCompute is a node that is a compute node in the cluster
	NodeTypeCompute NodeType = "Compute"
)
