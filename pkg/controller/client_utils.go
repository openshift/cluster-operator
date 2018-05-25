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
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/util/retry"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	"github.com/openshift/cluster-operator/pkg/logging"

	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

// PatchClusterDeploymentStatus will patch the cluster deployment with the difference
// between original and clusterDeployment. If the patch request fails due to a
// conflict, the request will be retried until it succeeds or fails for other
// reasons.
func PatchClusterDeploymentStatus(c clusteroperatorclientset.Interface, original, clusterDeployment *clusteroperator.ClusterDeployment) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return patchClusterDeploymentStatus(c, original, clusterDeployment)
	})
}

func patchClusterDeploymentStatus(c clusteroperatorclientset.Interface, oldClusterDeployment, newClusterDeployment *clusteroperator.ClusterDeployment) error {
	logger := logging.WithClusterDeployment(log.StandardLogger(), oldClusterDeployment)
	patchBytes, err := preparePatchBytesforClusterDeploymentStatus(oldClusterDeployment, newClusterDeployment)
	if err != nil {
		return err
	}

	// Do not send patch request if there is nothing to patch
	if string(patchBytes) == "{}" {
		return nil
	}

	logger.Debugf("about to patch clusterdeployment with %s", string(patchBytes))
	_, err = c.Clusteroperator().ClusterDeployments(newClusterDeployment.Namespace).Patch(newClusterDeployment.Name, types.StrategicMergePatchType, patchBytes, "status")
	if err != nil {
		logger.Warningf("Error patching clusterdeployment: %v", err)
	}
	return err
}

func preparePatchBytesforClusterDeploymentStatus(oldClusterDeployment, newClusterDeployment *clusteroperator.ClusterDeployment) ([]byte, error) {
	oldData, err := json.Marshal(oldClusterDeployment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oldData for clusterdeployment %s/%s: %v", oldClusterDeployment.Namespace, oldClusterDeployment.Name, err)
	}

	// Reset spec to make sure only patch for Status or ObjectMeta is generated.
	newClusterDeployment.Spec = oldClusterDeployment.Spec
	newData, err := json.Marshal(newClusterDeployment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal newData for clusterdeployment %s/%s: %v", newClusterDeployment.Namespace, newClusterDeployment.Name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, clusteroperator.ClusterDeployment{})
	if err != nil {
		return nil, fmt.Errorf("failed to create two way merge patch for clusterdeployment %s/%s: %v", newClusterDeployment.Namespace, newClusterDeployment.Name, err)
	}
	return patchBytes, nil
}

// UpdateClusterStatus will update the cluster status. If the update request
// fails due to a conflict, the request will NOT be retried.
func UpdateClusterStatus(c clusterclientset.Interface, cluster *clusterv1.Cluster) error {
	_, err := c.ClusterV1alpha1().Clusters(cluster.Namespace).UpdateStatus(cluster)
	return err
}
