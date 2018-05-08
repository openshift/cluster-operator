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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Annotation constants
const (
	// OwnerGenerationAnnotation contains the generation of the spec of the
	// owner of a job
	OwnerGenerationAnnotation = GroupName + "/owner.generation"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cluster represents a cluster that clusteroperator manages
type Cluster struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterSpec `json:"spec,omitempty"`
	// +optional
	Status ClusterStatus `json:"status,omitempty"`
}

// finalizer values unique to cluster-operator
const (
	FinalizerClusterOperator string = "openshift/cluster-operator"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterList is a list of Clusters.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Cluster `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterVersion represents a version of OpenShift that can be, or is installed.
type ClusterVersion struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterVersionSpec `json:"spec,omitempty"`
	// +optional
	Status ClusterVersionStatus `json:"status,omitempty"`
}

// ClusterVersionSpec is a specification of a cluster version that can be installed.
type ClusterVersionSpec struct {
	// ImageFormat defines a format string for the container registry and images to use for
	// various OpenShift components. Valid expansions are component (required, expands to
	// pod/deployer/haproxy-router/etc), and version (v3.9.0).
	// (i.e. example.com/openshift3/ose-${component}:${version}")
	ImageFormat string `json:"imageFormat"`

	VMImages VMImages `json:"vmImages"`

	// DeploymentType indicates the type of OpenShift deployment to create.
	DeploymentType ClusterDeploymentType `json:"deploymentType"`

	// Version is the version of OpenShift to install.
	Version string `json:"version"`

	// OpenshiftAnsibleImage is the name of the image to use to run
	// openshift-ansible playbooks.
	// Defaults to openshift/origin-ansbile:{TAG}, where {TAG} is
	// the value from the Version field of this ClusterVersion.
	// +optional
	OpenshiftAnsibleImage *string `json:"openshiftAnsibleImage,omitempty"`

	// OpenshiftAnsibleImagePullPolicy is the pull policy to use for
	// OpenshiftAnsibleImage.
	// Defaults to IfNotPreset.
	// +optional
	OpenshiftAnsibleImagePullPolicy *corev1.PullPolicy `json:"openshiftAnsibleImagePullPolicy,omitempty"`
}

// ClusterVersionStatus is the status of a ClusterVersion. It may be used to indicate if the
// cluster version is ready to be used, or if any problems have been detected.
type ClusterVersionStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterVersionList is a list of ClusterVersions.
type ClusterVersionList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterVersion `json:"items"`
}

// VMImages contains required data to locate images in all supported cloud providers.
type VMImages struct {
	// +optional
	AWSImages *AWSVMImages `json:"awsVMImages,omitempty"`
	// TODO: GCP, Azure added as necessary.
}

// AWSVMImages indicates which AMI to use in each supported AWS region for this OpenShift version.
type AWSVMImages struct {
	RegionAMIs []AWSRegionAMIs `json:"regionAMIs"`
}

// AWSRegionAMIs defines which AMI to use for a node type in a given region.
type AWSRegionAMIs struct {
	Region string `json:"region"`
	// AMI is the ID of the AMI to use for compute nodes in the cluster. If no MasterAMI is defined, it will be used for masters as well.
	AMI string `json:"ami"`
	// MasterAMI is the ID of the AMI to use for the master nodes in the cluster. If unset, the default AMI will be used instead.
	// +optional
	MasterAMI *string `json:"masterAMI,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterProviderConfigSpec is the cluster specification stored in the
// ProviderConfig of a cluster.k8s.io Cluster.
type ClusterProviderConfigSpec struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	ClusterSpec `json:",inline"`
}

// ClusterSpec is the specification of a cluster's hardware and configuration
type ClusterSpec struct {
	// Hardware specifies the hardware that the cluster will run on
	Hardware ClusterHardwareSpec `json:"hardware"`

	// Config specifies cluster-wide OpenShift configuration
	Config ClusterConfigSpec `json:"config"`

	// DefaultHardwareSpec specifies hardware defaults for all machine sets
	// in this cluster
	// +optional
	DefaultHardwareSpec *MachineSetHardwareSpec `json:"defaultHardwareSpec,omitempty"`

	// MachineSets specifies the configuration of all machine sets for the cluster
	MachineSets []ClusterMachineSet `json:"machineSets"`

	// ClusterVersionRef references the clusterversion that should be running.
	ClusterVersionRef ClusterVersionReference `json:"clusterVersionRef"`
}

// ClusterVersionReference provides information to locate a cluster version to use.
type ClusterVersionReference struct {
	// Namespace of the clusterversion.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the clusterversion.
	Name string `json:"name"`
}

// ClusterHardwareSpec specifies hardware for a cluster. The specification will
// be specific to each cloud provider.
type ClusterHardwareSpec struct {
	// AWS specifies cluster hardware configuration on AWS
	// +optional
	AWS *AWSClusterSpec `json:"aws,omitempty"`

	// TODO: Add other cloud-specific Specs as needed
}

// AWSClusterSpec contains cluster-wide configuration for a cluster on AWS
type AWSClusterSpec struct {
	// AccountSeceret refers to a secret that contains the AWS account access
	// credentials
	AccountSecret corev1.LocalObjectReference `json:"accountSecret"`

	// SSHSecret refers to a secret that contains the ssh private key to access
	// EC2 instances in this cluster.
	SSHSecret corev1.LocalObjectReference `json:"sshSecret"`

	// SSHUser refers to the username that should be used for Ansible to SSH to the system. (default: clusteroperator)
	// +optional
	SSHUser string `json:"sshUser"`

	// SSLSecret refers to a secret that contains the SSL certificate to use
	// for this cluster. The secret is expected to contain the following keys:
	// - ca.crt - the certificate authority certificate (optional)
	// - server.crt - the server certificate
	// - server.key - the server key
	SSLSecret corev1.LocalObjectReference `json:"sslSecret"`

	// KeyPairName is the name of the AWS key pair to use for SSH access to EC2
	// instances in this cluster
	KeyPairName string `json:"keyPairName"`

	// Region specifies the AWS region where the cluster will be created
	Region string `json:"region"`

	// VPCName specifies the name of the VPC to associate with the cluster.
	// If a value is specified, a VPC will be created with that name if it
	// does not already exist in the cloud provider. If it does exist, the
	// existing VPC will be used.
	// If no name is specified, a VPC name will be generated using the
	// cluster name and created in the cloud provider.
	// +optional
	VPCName string `json:"vpcName,omitempty"`

	// VPCSubnet specifies the subnet to use for the cluster's VPC. Only used
	// when a new VPC is created for the cluster
	// +optional
	VPCSubnet string `json:"vpcSubnet,omitempty"`
}

// ClusterConfigSpec contains OpenShift configuration for a cluster
type ClusterConfigSpec struct {
	// SDNPluginName is the name of the SDN plugin to use for this install
	SDNPluginName string `json:"sdnPluginName"`

	// ServiceNetworkSubnet is the CIDR to use for service IPs in the cluster
	// +optional
	ServiceNetworkSubnet string `json:"serviceNetowrkSubnet,omitempty"`

	// PodNetworkSubnet is the CIDR to use for pod IPs in the cluster
	// +optional
	PodNetworkSubnet string `json:"podNetworkSubnet,omitempty"`
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
	MachineSetCount int `json:"machineSetCount"`

	// MasterMachineSetName is the name of the master machine set
	MasterMachineSetName string `json:"masterMachineSetName,omitempty"`

	// InfraMachineSetName is the name of the infra machine set
	InfraMachineSetName string `json:"infraMachineSetName,omitempty"`

	// AdminKubeconfig points to a secret containing a cluster administrator
	// kubeconfig to access the cluster. The secret can be used for bootstrapping
	// and subsequent access to the cluster API.
	// +optional
	AdminKubeconfig *corev1.LocalObjectReference `json:"adminKubeconfig,omitempty"`

	// Provisioned is true if the hardware pre-reqs for the cluster have been provisioned
	// For machine set hardware, see the status of each machine set resource.
	Provisioned bool `json:"provisioned"`

	// ProvisionedJobGeneration is the generation of the cluster resource used to
	// generate the latest completed infra provisioning job.
	ProvisionedJobGeneration int64 `json:"provisionedJobGeneration"`

	// Ready is true if the master of the cluster is ready to be used and can be accessed using
	// the KubeconfigSecret
	Ready bool `json:"ready"`

	// Conditions includes more detailed status for the cluster
	Conditions []ClusterCondition `json:"conditions"`

	// ClusterVersionRef references the resolved clusterversion the cluster should be running.
	// +optional
	ClusterVersionRef *corev1.ObjectReference `json:"clusterVersionRef,omitempty"`
}

// ClusterCondition contains details for the current condition of a cluster
type ClusterCondition struct {
	// Type is the type of the condition.
	Type ClusterConditionType `json:"type"`
	// Status is the status of the condition.
	Status corev1.ConditionStatus `json:"status"`
	// LastProbeTime is the last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// LastTransitionTime is the last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Reason is a unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
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
	// ClusterVersionIncompatible is true when the cluster version does not have an AMI defined for the cluster's region.
	ClusterVersionIncompatible ClusterConditionType = "VersionIncompatible"
	// ClusterReady means the cluster is able to service requests
	ClusterReady ClusterConditionType = "Ready"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineSet represents a group of machines in a cluster that clusteroperator manages
type MachineSet struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// Spec is the specification for the MachineSet
	// +optional
	Spec MachineSetSpec `json:"spec,omitempty"`

	// Status is the status for the MachineSet
	// +optional
	Status MachineSetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineSetList is a list of MachineSets.
type MachineSetList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []MachineSet `json:"items"`
}

// ClusterMachineSet is the specification of a machine set in a cluster
type ClusterMachineSet struct {
	// ShortName is a unique name for the machine set within the cluster
	ShortName string `json:"shortName"`

	// MachineSetConfig is the configuration for the MachineSet
	MachineSetConfig `json:",inline"`
}

// MachineSetConfig contains configuration for a MachineSet
type MachineSetConfig struct {
	// NodeType is the type of nodes that comprise the MachineSet
	NodeType NodeType `json:"nodeType"`

	// Infra indicates whether this machine set should contain infrastructure
	// pods
	Infra bool `json:"infra"`

	// Size is the number of nodes that the node group should contain
	Size int `json:"size"`

	// Hardware defines what the hardware should look like for this
	// MachineSet. The specification will vary based on the cloud provider.
	// +optional
	Hardware *MachineSetHardwareSpec `json:"hardware,omitempty"`

	// NodeLabels specifies the labels that will be applied to nodes in this
	// MachineSet
	NodeLabels map[string]string `json:"nodeLabels"`
}

// MachineSetSpec is the specification for a MachineSet
type MachineSetSpec struct {
	// MachineSetConfig is the configuration for the MachineSet
	MachineSetConfig `json:",inline"`

	// ClusterHardware specifies the hardware that the cluster will run on
	ClusterHardware ClusterHardwareSpec `json:"clusterHardware"`

	// ClusterVersionRef references the clusterversion the machine set is running.
	ClusterVersionRef corev1.ObjectReference `json:"clusterVersionRef"`
}

// MachineSetHardwareSpec specifies the hardware for a MachineSet
type MachineSetHardwareSpec struct {
	// AWS specifies the hardware spec for an AWS machine set
	// +optional
	AWS *MachineSetAWSHardwareSpec `json:"aws,omitempty"`
}

// MachineSetAWSHardwareSpec specifies AWS hardware for a MachineSet
type MachineSetAWSHardwareSpec struct {
	// InstanceType is the type of instance to use for machines in this MachineSet
	// +optional
	InstanceType string `json:"instanceType,omitempty"`
}

// MachineSetStatus is the status of a MachineSet
type MachineSetStatus struct {
	// MachinesProvisioned is the count of provisioned machines for the MachineSet
	MachinesProvisioned int `json:"machineCount"`

	// MachinesReady is the number of machines that are ready
	MachinesReady int `json:"machinesReady"`

	// Conditions includes more detailed status of the MachineSet
	Conditions []MachineSetCondition `json:"conditions"`

	// ComponentsInstalled is true if the additional components needed for the cluster
	// have been installed
	ComponentsInstalled bool `json:"componentsInstalled"`

	// ComponentsInstalledJobGeneration is the generation of the machine set resource used to
	// generate the latest completed component installation job.
	ComponentsInstalledJobGeneration int64 `json:"componentsInstalledJobGeneration"`

	// NodeConfigInstalled is true if the node config daemonset has been created in the cluster.
	NodeConfigInstalled bool `json:"nodeConfigInstalled"`

	// NodeConfigInstalledJobGeneration is the generation of the machine set resource used to generate
	// the latest successful node config installation job.
	NodeConfigInstalledJobGeneration int64 `json:"nodeConfigInstalledJobGeneration"`

	// NodeConfigInstalledTime is the time we last successfully installed the node config daemonset on the cluster.
	NodeConfigInstalledTime *metav1.Time `json:"nodeConfigLastInstalled"`

	// ClusterAPIInstalled is true if the Kubernetes Cluster API controllers have been deployed onto the remote cluster.
	ClusterAPIInstalled bool `json:"clusterAPIInstalled"`

	// ClusterAPIInstalledJobGeneration is the generation of the machine set resource used to
	// generate the latest completed deployclusterapi installation job.
	ClusterAPIInstalledJobGeneration int64 `json:"clusterAPIInstalledJobGeneration"`

	// ClusterAPIInstalledTime is the time we last successfully deployed the Kubernetes Cluster API controllers on the cluster.
	ClusterAPIInstalledTime *metav1.Time `json:"clusterAPIInstalledTime"`

	// Installed is true if the software required for this machine set is installed
	// and running.
	Installed bool `json:"installed"`

	// InstalledJobGeneration is the generation of the machine set resource used to
	// generate the latest completed installation job.
	InstalledJobGeneration int64 `json:"installedJobGeneration"`

	// Provisioned is true if the hardware that corresponds to this MachineSet has
	// been provisioned
	Provisioned bool `json:"provisioned"`

	// ProvisionedJobGeneration is the generation of the machine set resource used to
	// generate the latest completed hardware provisioning job.
	ProvisionedJobGeneration int64 `json:"provisionedJobGeneration"`

	// Accepted is true if machine set nodes have been accepted on the master
	Accepted bool `json:"accepted"`

	// AcceptedJobGeneration is the generation of the machine set resource used to
	// run the latest completed accept job.
	AcceptedJobGeneration int64 `json:"acceptedJobGeneration"`
}

// MachineSetCondition contains details for the current condition of a MachineSet
type MachineSetCondition struct {
	// Type is the type of the condition.
	Type MachineSetConditionType `json:"type"`
	// Status is the status of the condition.
	Status corev1.ConditionStatus `json:"status"`
	// LastProbeTime is the last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// LastTransitionTime is the last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Reason is a unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
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

	// MachineSetHardwareDeprovisioning is true if the cloud resources for this
	// machine set are in the process of deprovisioning.
	MachineSetHardwareDeprovisioning MachineSetConditionType = "HardwareDeprovisioning"

	// MachineSetHardwareDeprovisioningFailed is true if the last deprovisioning attempt
	// for this machine set failed.
	MachineSetHardwareDeprovisioningFailed MachineSetConditionType = "HardwareDeprovisioningFailed"

	// MachineSetInstalling is true if OpenShift is being installed on
	// this machine set.
	MachineSetInstalling MachineSetConditionType = "Installing"

	// MachineSetInstallationFailed is true if the installation of
	// OpenShift on this machine set failed.
	MachineSetInstallationFailed MachineSetConditionType = "InstallationFailed"

	// MachineSetInstalled is true if OpenShift has been installed
	// on this machine set.
	MachineSetInstalled MachineSetConditionType = "Installed"

	// MachineSetComponentsInstalling is true if OpenShift components are being installed on
	// this machine set.
	MachineSetComponentsInstalling MachineSetConditionType = "ComponentsInstalling"

	// MachineSetComponentsInstallationFailed is true if the installation of
	// OpenShift components on this machine set failed.
	MachineSetComponentsInstallationFailed MachineSetConditionType = "ComponentsInstallationFailed"

	// MachineSetComponentsInstalled is true if OpenShift has been installed
	// on this machine set.
	MachineSetComponentsInstalled MachineSetConditionType = "ComponentsInstalled"

	// MachineSetNodeConfigInstalling is true if node config daemonset is being installed.
	MachineSetNodeConfigInstalling MachineSetConditionType = "NodeConfigInstalling"

	// MachineSetNodeConfigInstallationFailed is true if the node config daemonset failed to be installed.
	MachineSetNodeConfigInstallationFailed MachineSetConditionType = "NodeConfigInstallationFailed"

	// MachineSetNodeConfigInstalled is true if the node config daemonset has been installed.
	MachineSetNodeConfigInstalled MachineSetConditionType = "NodeConfigInstalled"

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

	// MachineSetClusterAPIInstalling is true if the Kubernetes Cluster API controllers are being installed on
	// the remote cluster.
	MachineSetClusterAPIInstalling MachineSetConditionType = "ClusterAPIInstalling"

	// MachineSetClusterAPIInstallationFailed is true if the installation of the Kubernetes Cluster API controllers failed.
	MachineSetClusterAPIInstallationFailed MachineSetConditionType = "ClusterAPIInstallationFailed"

	// MachineSetClusterAPIInstalled is true if the Kubernetes Cluster API controllers have been successfully installed.
	MachineSetClusterAPIInstalled MachineSetConditionType = "ClusterAPIInstalled"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Machine represents a node in a cluster that clusteroperator manages
type Machine struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec MachineSpec `json:"spec,omitempty"`
	// +optional
	Status MachineStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineList is a list of Nodes.
type MachineList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Machine `json:"items"`
}

// MachineSpec is the specificiation of a Machine.
type MachineSpec struct {
	// NodeType is the type of the node
	NodeType NodeType `json:"nodeType"`
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
