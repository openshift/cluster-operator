# Cluster Operator API

## Overview

The cluster operator API defines a set of resources that allow the instantiation of OpenShift clusters and subsequent
updates to those clusters based on changes to said resources.

The general approach for these resources is to follow the Kubernetes model of a spec section that is end-user controlled and
a status section that represents the current state of the resources. Changes to the spec will result in the necessary actions
by the system to reconcile the current state of the system with what the user has specified.

## Resources


The cluster resource should allow 2 levels/scopes of spec:
- Cluster-wide configuration
- NodeGroup configuration

Furthermore, each scope can be described in 2 parts:
- Hardware spec (needed to instantiate the proper virtual hardware)
- OpenShift config (openshift-specific configuration)

For the Status part, if the cluster has been instantiated, we need to know how to access it (Kubeconfig may be sufficient)

In more detail:

### Cluster

- `ObjectMetadata (inline) [metav1.ObjectMetadata]`
- `TypeMetadata (inline) [metav1.TypeMetadata]`

#### Spec:
- `Hardware`
  - `AWS: [struct, optional]` *AWS-specific hardware configuration for the cluster*
    - `AccountSecret [corev1.LocalReference]` *Reference to a secret that contains an AWS access key id and secret access key*
    - `SSHKeyName [string]` *SSH secret name to use for machines in this cluster (ie. 'libra')*
    - `SSHSecret [corev1.LocalReference]` *Reference to a secret that contains a SSH private key to SSH into the cluster*
    - `Region [string]` *Region where the cluster will be created (us-east-1 for example)*
    - `VPCName [string, optional]` *Name of the VPC to use for this cluster (or create if not present)*
    - `VPCSubnet [string]` *Subnet of the VPC (only used if creating a new one)*
- OpenShift Configuration: *Configuration of the cluster that's not cloud-provider dependent*
  - `Version [corev1.ObjectReference]` *Reference to a ClusterVersion resource (defined below) for the cluster*
  - `PublicHostname [string]` *Public hostname of the cluster (openshift_master_cluster_public_hostname in ansible)*
  - `ServiceNetworkCIDR [array of string]` *Subnet for Kube service IPs (openshift_portal_net in ansible)*
  - `SDNConfig` *Configuration specific to SDN to use, if the SDN requires additional options, they will be added as optional struct fields to this struct*
    - `PluginName [string]` *Name of SDN plugin to use (os_sdn_network_plugin_name in ansible). Possible values include but are not limited to `openshift/openshift-ovs-multitenant`, `openshift/openshift-ovs-subnet`, `openshift/openshift-network-policy`*
    - `PodNetworkCIDR [array of string]` *Subnet for pod network (osm_cluster_network_cidr in ansible).*
- `MachineSets [array of ClusterMachineSet]` *List of MachineSets to create for this cluster*
  - `Name [string, required]` *Name for the machine set - only used to identify the machine set within the cluster. Must be unique within the cluster*
  - `MachineSetConfig [MachineSetConfig,inline]` *Configuration for the machine set -- see below*

#### Status:
- `AdminKubeconfig [corev1.LocalReference]` *Reference to secret that contains an admin.kubeconfig to access the cluster as cluster administrator after the cluster is up and running*
- `Conditions [array]` *Detailed status for the cluster*
  - `Type [string]` *Type of condition - InfraProvisioned, MasterConfigured, Running*
  - `Status [string]` *Status of the condition - True, False, Unknown*
  - `LastProbeTime [string]` *-optional- last time the condition was checked*
  - `LastTransitionTime [string]` *-optional- last time the condition transitioned*
  - `Message [string]` *-optional- Human readable message associated with the condition*
  - `Reason [string]` *-optional- Single word camel-case reason for the condition*
- `MachineSetCount [int]` *count of machine sets that have been created for this cluster*
- `Provisioned [boolean]` *true if cluster-level infrastructure has been provisioned for this cluster*
- `Running [boolean]` *true if the cluster is running and is ready to accept API requests* 
- `MasterMachineSetName [string]` *name of the machine set designated as master*
- `InfraMachineSetName [string]` *name of the machine set designated as infra (could be the same as master)*

### MachineSet

- `ObjectMetadata [metav1.ObjectMetadata]`
- `TypeMetadata [metav1.TypeMetadata]`

#### Spec
- `MachineSetConfig [MachineSetConfig,inline]` *Configuration for the machine set (see below)*

#### Status
- `NodesProvisioned [int]` *Number of nodes provisioned by the cloud provider*
- `NodesReady [int]` *Number of nodes ready*
- `Conditions [array]` *Detailed status*
  - `Type [string]` *Type of condition - Provisioned, Ready, etc*
  - `Status [string]` *Status of the condition - True, False, Unknown*
  - `LastProbeTime [string]` *-optional- last time the condition was checked*
  - `LastTransitionTime [string]` *-optional- last time the condition transitioned*
  - `Message [string]` *-optional- Human readable message associated with the condition*
  - `Reason [string]` *-optional- Single word camel-case reason for the condition*


### MachineSetConfig
  - `NodeType [NodeType:string]` *Enumerated node type for the machine set (compute or master)*
  - `Infra [boolean]` *True if this machine set should contain infrastructure pods (registry, router)*
  - `Size [int]` *How many machines should be created for this machine set*
  - `Version [corev1.LocalObjectReference]` *Reference to ClusterVersion resource*
  - `Hardware`
    - `AWS` *-optional- contains AWS specific configuration*
      - `InstanceType [string]` *EC2 instance type (ie. m4.xlarge)*
      - `AMIName [string]` *Name of AMI used to create this machine set (derived from Version object)*
  - `NodeLabels [map[string]string]` *Labels to apply to nodes created in this MachineSet*

### ClusterVersion
Standalone resource that encapsulates details of a named version. It
defines general OpenShift details about a particular version such as 
the image template and RPM version as well as cloud-specific information
such as the AWS AMI name.

A set of predefined ClusterVersion resources can be initially created in a shared 
namespace (like `openshift`) to define the versions supported out of the box.
Users are able to create their own in their own namespace and associate their
clusters with that.

- `ObjectMetadata (inline) [metav1.ObjectMetadata]`
- `TypeMetadata (inline) [metav1.TypeMetadata]`

#### Spec
- `ImageURL [string]` *-optional- template for images to be used on the cluster (oreg_url in ansible)*
- `ImageTag [string]` *-optional- the image tag for OpenShift images (either ImageURL or ImageTag can be specified, but not both, openshift_image_tag in ansible)*
- `RPMRepositories [array]` *-optional- description of RPM repositories to use in the cluster (openshift_additional_repos in ansible)*
  - `URL [string]` *URL to the RPM repository*
  - `GPGKey [string]` *URL to GPG key for repository*
- `PackageVersion [string]` *-optional- RPM package version (openshift_pkg_version in ansible)*
- `Hardware` *contains cloud-provider specific configuration (AWS for now)*
  - `AWS` 
    - `AMIName [string]` *name of the AMI that corresponds to this version*
