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

package controller

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

// CombinedClusterForClusterAPICluster creates a CombinedCluster
// for the specified cluster-api Cluster.
func CombinedClusterForClusterAPICluster(cluster *clusterapi.Cluster) (*clusteroperator.CombinedCluster, error) {
	clusterOperatorSpec, err := ClusterDeploymentSpecFromCluster(cluster)
	if err != nil {
		return nil, err
	}
	clusterOperatorStatus, err := ClusterStatusFromClusterAPI(cluster)
	if err != nil {
		return nil, err
	}
	return &clusteroperator.CombinedCluster{
		TypeMeta:                cluster.TypeMeta,
		ObjectMeta:              cluster.ObjectMeta,
		ClusterDeploymentSpec:   clusterOperatorSpec,
		ClusterDeploymentStatus: clusterOperatorStatus,
		ClusterSpec:             &cluster.Spec,
		ClusterStatus:           &cluster.Status,
	}, nil
}

// ClusterAPIClusterForCombinedCluster creates a cluster-api Cluster from the
// specified CombinedCluster.
// If ignoreChanges is true, then the ProviderStatus will not be modified with
// the cluster-operator ClusterStatus. This is useful for re-creating the original
// cluster-api Cluster that was used to create the CombinedCluster.
func ClusterAPIClusterForCombinedCluster(cluster *clusteroperator.CombinedCluster, ignoreChanges bool) (*clusterapi.Cluster, error) {
	newCluster := &clusterapi.Cluster{
		TypeMeta:   cluster.TypeMeta,
		ObjectMeta: cluster.ObjectMeta,
		Spec:       *cluster.ClusterSpec,
		Status:     *cluster.ClusterStatus,
	}
	if !ignoreChanges {
		// We don't bother replacing ProviderConfig since we will not be updating the
		// cluster spec.
		providerStatus, err := ClusterAPIProviderStatusFromClusterStatus(cluster.ClusterDeploymentStatus)
		if err != nil {
			return nil, err
		}
		newCluster.Status.ProviderStatus = providerStatus
	}
	return newCluster, nil
}

// ConvertToCombinedCluster converts a Cluster object, that is either a
// cluster-operator Cluster, a cluster-api Cluster, or a CombinedCluster, into
// a CombinedCluster object.
func ConvertToCombinedCluster(cluster metav1.Object) (*clusteroperator.CombinedCluster, error) {
	switch c := cluster.(type) {
	case *clusteroperator.CombinedCluster:
		return c, nil
	case *clusterapi.Cluster:
		return CombinedClusterForClusterAPICluster(c)
	default:
		return nil, fmt.Errorf("unknown type %T", c)
	}
}
