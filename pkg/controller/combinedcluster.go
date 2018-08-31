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

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

// CombinedClusterForClusterAPICluster creates a CombinedCluster
// for the specified cluster-api Cluster.
func CombinedClusterForClusterAPICluster(cluster *capiv1.Cluster) (*cov1.CombinedCluster, error) {
	awsSpec, err := AWSClusterProviderConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}
	clusterOperatorStatus, err := ClusterProviderStatusFromCluster(cluster)
	if err != nil {
		return nil, err
	}
	return &cov1.CombinedCluster{
		TypeMeta:                 cluster.TypeMeta,
		ObjectMeta:               cluster.ObjectMeta,
		AWSClusterProviderConfig: awsSpec,
		ClusterProviderStatus:    clusterOperatorStatus,
		ClusterSpec:              &cluster.Spec,
		ClusterStatus:            &cluster.Status,
	}, nil
}

// ClusterAPIClusterForCombinedCluster creates a cluster-api Cluster from the
// specified CombinedCluster.
// If ignoreChanges is true, then the ProviderStatus will not be modified with
// the cluster-operator ClusterStatus. This is useful for re-creating the original
// cluster-api Cluster that was used to create the CombinedCluster.
func ClusterAPIClusterForCombinedCluster(cluster *cov1.CombinedCluster, ignoreChanges bool) (*capiv1.Cluster, error) {
	newCluster := &capiv1.Cluster{
		TypeMeta:   cluster.TypeMeta,
		ObjectMeta: cluster.ObjectMeta,
		Spec:       *cluster.ClusterSpec,
		Status:     *cluster.ClusterStatus,
	}
	if !ignoreChanges {
		// We don't bother replacing ProviderConfig since we will not be updating the
		// cluster spec.
		providerStatus, err := EncodeClusterProviderStatus(cluster.ClusterProviderStatus)
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
func ConvertToCombinedCluster(cluster metav1.Object) (*cov1.CombinedCluster, error) {
	switch c := cluster.(type) {
	case *cov1.CombinedCluster:
		return c, nil
	case *capiv1.Cluster:
		return CombinedClusterForClusterAPICluster(c)
	default:
		return nil, fmt.Errorf("unknown type %T", c)
	}
}
