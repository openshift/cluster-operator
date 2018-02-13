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
	// ClusterGenerationAnnotation contains the generation of a cluster spec used to create
	// a provisioning job
	ClusterGenerationAnnotation = GroupName + "/cluster.generation"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cluster represents a cluster that clusteroperator manages
type Cluster struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// +optional
	Spec ClusterSpec
	// +optional
	Status ClusterStatus
}

// finalizer values unique to cluster-operator
const (
	FinalizerClusterOperator string = "openshift/cluster-operator"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterList is a list of Clusters.
type ClusterList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []Cluster
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
	// +optional
	YumRepositories []YumRepository

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

// YumRepository represents optional yum repositories to deploy onto all systems in the cluster.
type YumRepository struct {
	ID      string
	Name    string
	BaseURL string
	// GPGCheck controls whether or not the packages in the repository should have their GPG signatures validated. Must be 0 or 1 to match yum.
	Enabled int
	// GPGCheck controls whether or not the packages in the repository should have their GPG signatures validated. Must be 0 or 1 to match yum.
	GPGCheck int
	// +optional
	GPGKey string
}

// ClusterSpec is the specification of a cluster's hardware and configuration
type ClusterSpec struct {
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
	// EC2 instances in this cluster
	SSHSecret corev1.LocalObjectReference

	// SSHUser refers to the username that should be used for Ansible to SSH to the system. (default: clusteroperator)
	// +optional
	SSHUser string

	// SSLSecret refers to a secret that contains the SSL certificate to use
	// for this cluster. The secret is expected to contain the following keys:
	// - ca.crt - the certificate authority certificate
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

// ClusterStatus contains the status for a cluster
type ClusterStatus struct {
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
	// to generate the latest completed infra provisioning job. The value will be set
	// regardless of the job having succeeded or failed.
	ProvisionedJobGeneration int64

	// ProvisionJob is the job that is actively performing provisioning
	// on the cluster.
	// +optional
	ProvisionJob *corev1.LocalObjectReference

	// Running is true if the master of the cluster is running and can be accessed using
	// the KubeconfigSecret
	Running bool

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
	// ClusterReady means the cluster is able to service requests
	ClusterReady ClusterConditionType = "Ready"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineSet represents a group of machines in a cluster that clusteroperator manages
type MachineSet struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// Spec is the specification for the MachineSet
	// +optional
	Spec MachineSetSpec

	// Status is the status for the MachineSet
	// +optional
	Status MachineSetStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineSetList is a list of MachineSets.
type MachineSetList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []MachineSet
}

// ClusterMachineSet is the specification of a machine set in a cluster
type ClusterMachineSet struct {
	// Name is a unique name for the machine set within the cluster
	Name string

	// MachineSetConfig is the configuration for the MachineSet
	MachineSetConfig
}

// MachineSetConfig contains configuration for a MachineSet
type MachineSetConfig struct {
	// NodeType is the type of nodes that comprise the MachineSet
	NodeType NodeType

	// Infra indicates whether this machine set should contain infrastructure
	// pods
	Infra bool

	// Size is the number of nodes that the node group should contain
	Size int

	// Hardware defines what the hardware should look like for this
	// MachineSet. The specification will vary based on the cloud provider.
	// +optional
	Hardware *MachineSetHardwareSpec

	// NodeLabels specifies the labels that will be applied to nodes in this
	// MachineSet
	NodeLabels map[string]string
}

// MachineSetSpec is the specification for a MachineSet
type MachineSetSpec struct {
	// MachineSetConfig is the configuration for the MachineSet
	MachineSetConfig

	// ClusterHardware specifies the hardware that the cluster will run on
	ClusterHardware ClusterHardwareSpec

	// ClusterVersionRef references the clusterversion the machine set is running.
	ClusterVersionRef corev1.ObjectReference
}

// MachineSetHardwareSpec specifies the hardware for a MachineSet
type MachineSetHardwareSpec struct {
	// AWS specifies the hardware spec for an AWS MachineSet
	// +optional
	AWS *MachineSetAWSHardwareSpec
}

// MachineSetAWSHardwareSpec specifies AWS hardware for a MachineSet
type MachineSetAWSHardwareSpec struct {
	// InstanceType is the type of instance to use for machines in this MachineSet
	// +optional
	InstanceType string

	// AMIName is the name of the AMI to use for machines in this MachineSet
	// +optional
	AMIName string
}

// MachineSetStatus is the status of a MachineSet
type MachineSetStatus struct {
	// MachinesProvisioned is the count of provisioned machines for the MachineSet
	MachinesProvisioned int

	// MachinesReady is the number of nodes that are ready
	MachinesReady int

	// Conditions includes more detailed status of the MachineSet
	Conditions []MachineSetCondition

	// Installed is true if the software required for this machine set is installed
	// and running.
	Installed bool

	// InstalledJobGeneration is the generation of the machine set resource used to
	// to generate the latest completed installation job. The value will be set
	// regardless of the job having succeeded or failed.
	InstalledJobGeneration int64

	// InstallationJob is the job that is actively performing installation
	// on the machine set.
	// +optional
	InstallationJob *corev1.LocalObjectReference

	// Provisioned is true if the hardware that corresponds to this MachineSet has
	// been provisioned
	Provisioned bool

	// ProvisionedJobGeneration is the generation of the machine set resource used to
	// to generate the latest completed hardware provisioning job. The value will be set
	// regardless of the job having succeeded or failed.
	ProvisionedJobGeneration int64

	// ProvisionJob is the job that is actively performing provisioning
	// on the machine set.
	// +optional
	ProvisionJob *corev1.LocalObjectReference

	// Accepted is true if machine set nodes have been accepted on the master
	Accepted bool

	// AcceptedJobGeneration is the generation of the machine set resource used to
	// run the latest completed accept job. The value will be set regardless of
	// the job having succceeded or failed.
	AcceptedJobGeneration int64

	// AcceptJob is the job that is actively running to accept nodes from this machine
	// set on the master.
	// +optional
	AcceptJob *corev1.LocalObjectReference
}

// MachineSetCondition contains details for the current condition of a MachineSet
type MachineSetCondition struct {
	// Type is the type of the condition.
	Type MachineSetConditionType
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

// MachineSetConditionType is a valid value for MachineSetCondition.Type
type MachineSetConditionType string

// These are valid conditions for a node group
const (
	// MachineSetHardwareProvisioning is true if the cloud resources for this
	// machine set are in the process of provisioning.
	MachineSetHardwareProvisioning MachineSetConditionType = "HardwareProvisioning"

	// MachineSetHardwareProvisioningFailed is true if the last provisioning attempt
	// for this machine set failed.
	MachineSetHardwareProvisioningFailed MachineSetConditionType = "HardwareProvisioningFailed"

	// MachineSetHardwareProvisioned is true if the corresponding cloud resource(s) for
	// this machine set have been provisioned (ie. AWS autoscaling group)
	MachineSetHardwareProvisioned MachineSetConditionType = "HardwareProvisioned"

	// MachineSetInstalling is true if OpenShift is being installed on
	// this machine set.
	MachineSetInstalling MachineSetConditionType = "Installing"

	// MachineSetInstallationFailed is true if the installation of
	// OpenShift on this machine set failed.
	MachineSetInstallationFailed MachineSetConditionType = "InstallationFailed"

	// MachineSetInstalled is true if OpenShift has been installed
	// on this machine set.
	MachineSetInstalled MachineSetConditionType = "Installed"

	// MachineSetHardwareReady is true if the hardware for the nodegroup is in ready
	// state (is started and healthy)
	MachineSetHardwareReady MachineSetConditionType = "HardwareReady"

	// MachineSetReady is true if the nodes of this nodegroup are ready for work
	// (have joined the cluster and have a healthy node status)
	MachineSetReady MachineSetConditionType = "Ready"

	// MachineSetAccepting is true if the cluster is in the process of accepting the nodes from this machine set.
	MachineSetAccepting MachineSetConditionType = "Accepting"

	// MachineSetAcceptFailed is true when the job to accept nodes from this machine set has failed.
	MachineSetAcceptFailed MachineSetConditionType = "AcceptFailed"

	// MachineSetAccepted is true when nodes from this machine set have been accepted by the master of the cluster.
	MachineSetAccepted MachineSetConditionType = "Accepted"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Machine represents a member of a MachineSet in a cluster that clusteroperator manages
type Machine struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// +optional
	Spec MachineSpec
	// +optional
	Status MachineStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineList is a list of Machines.
type MachineList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []Machine
}

// MachineSpec is the specificiation of a Machine.
type MachineSpec struct {
	// NodeType is the type of the node in this machine
	NodeType NodeType
}

// MachineStatus is the status of a Machine.
type MachineStatus struct {
}

// NodeType is the type of the Node
type NodeType string

const (
	// NodeTypeMaster is a node that is a master in the cluster
	NodeTypeMaster NodeType = "Master"
	// NodeTypeCompute is a node that is a compute node in the cluster
	NodeTypeCompute NodeType = "Compute"
)
