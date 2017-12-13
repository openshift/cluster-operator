# Cluster Operator API

## Overview

The cluster operator API defines a set of resources that allow the instantiation of OpenShift clusters and subsequent
updates to those clusters based on changes to said resources.

The general approach for these resources is to follow the Kubernetes model of a spec section that is end-user controlled and
a status section that represents the current state of the resources. Changes to the spec will result in the necessary actions
by the system to reconcile the current state of the system with what the user has specified.

## Resources

### Cluster

The cluster resource should allow 2 levels/scopes of spec:
- Cluster-wide configuration
- NodeGroup configuration

Furthermore, each scope can be described in 2 parts:
- Hardware spec (needed to instantiate the proper virtual hardware)
- Config (openshift-specific configuration)

For the Status part, if the cluster has been instantiated, we need to know how to access it (Kubeconfig may be sufficient)

In more detail:

#### Spec:
* Cluster-wide configuration:
  * ObjectMetadata (inline ObjectMetadata)
  * TypeMetadata (inline TypeMetadata)
  * Hardware-spec
    * Type (AWS, Azure, GCE, etc) [string]
    * AWSConfig: [struct]
      - AWSSecret [LocalReference]
      - Region [string]
      - VPCName [string]
      - VPCSubnet [string]
  * OpenShift Configuration:
    * DeploymentType [string]
    * OpenShiftVersion [string]
    * Public hostname [string]
    * SDN Plugin Name [string]
    * Service Network CIDR [string]
    * Pod Network CIDR [string]
    * Variables [array of Variable struct]
* NodeGroups [array of NodeGroupSpec]:
  - Type: (Master, Compute) [string]
  - Infra: [boolean]
  - Count [usigned int]
  - Hardware spec (cloud-specific):
    - AWSConfig:
      - InstanceType [string]
      - AMIName [string]
  - OpenShift Configuration:
    - NodeLabels [array of String]

#### Status:
* Access
  - KubeConfigSecret [LocalReference]
* Conditions [array]
  - Type (HardwareReady, MasterReady, etc.) [string]
  - Status (True/False/Unknown) [string]
  - LastProbeTime [string]
  - LastTransitionTime [string]
  - Message [string]
  - Reason [string]
* NodeGroupCounts [array]
  - Name [string]
  - Count [unsigned int]
* Phase [string]


### NodeGroup

#### Spec
- Type (Master/Compute) [string]
- Infra [boolean]
- Count [unsigned int]
- Hardware spec (cloud-specific):
  - AWSConfig:
    - InstanceType [string]
    - AMIName [string]
- OpenShift Configuration:
  - NodeLabels [array of string]

#### Status
* NodeCount [int]
* NodesReady [int]
* Conditions [array of Condition]
  - Type (Provisioning, Resizing, Ready) [string]
  - Status (True/False/Unknown) [string]
  - LastProbeTime [string]
  - LastTransitionTime [string]
  - Message [string]
  - Reason [string]
* Phase [string]
