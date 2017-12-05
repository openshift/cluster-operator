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

package boatswain

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cluster represents a cluster that boatswain manages
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

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterList is a list of Clusters.
type ClusterList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []Cluster
}

type ClusterSpec struct {
	MasterNodes ClusterNodeGroup

	// +optional
	ComputeNodeGroups []ClusterNodeGroup
}

type ClusterStatus struct {
}

// ClusterNodeGroup is a node group defined in a Cluster resource
type ClusterNodeGroup struct {
	Size int
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeGroup represents a group of nodes in a cluster that boatswain manages
type NodeGroup struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// +optional
	Spec NodeGroupSpec
	// +optional
	Status NodeGroupStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeGroupList is a list of NodeGroups.
type NodeGroupList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []NodeGroup
}

type NodeGroupSpec struct {
	ClusterName string

	// NodeType is the type of nodes that comprised the NodeGroup
	NodeType NodeType
}

type NodeGroupStatus struct {
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Node represents a node in a cluster that boatswain manages
type Node struct {
	// +optional
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// +optional
	Spec NodeSpec
	// +optional
	Status NodeStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeList is a list of Nodes.
type NodeList struct {
	metav1.TypeMeta
	// +optional
	metav1.ListMeta

	Items []Node
}

type NodeSpec struct {
	NodeGroupName string `json:"nodeGroupName"`

	// NodeType is the type of the node
	NodeType NodeType
}

type NodeStatus struct {
}

// NodeType is the type of the Node
type NodeType string

const (
	// NodeTypeMaster is a node that is a master in the cluster
	NodeTypeMaster NodeType = "Master"
	// NodeTypeCompute is a node that is a compute node in the cluster
	NodeTypeCompute NodeType = "Compute"
)
