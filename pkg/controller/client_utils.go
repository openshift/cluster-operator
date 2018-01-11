package controller

import (
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
)

func PatchClusterStatus(c clusteroperatorclientset.Interface, oldCluster, newCluster *clusteroperator.Cluster) error {
	logger := log.WithField("cluster", fmt.Sprintf("%s/%s", oldCluster.Namespace, oldCluster.Name))
	patchBytes, err := preparePatchBytesforClusterStatus(oldCluster, newCluster)
	if err != nil {
		return err
	}
	logger.Debugf("about to patch cluster with %s", string(patchBytes))
	_, err = c.Clusteroperator().Clusters(newCluster.Namespace).Patch(newCluster.Name, types.StrategicMergePatchType, patchBytes, "status")
	if err != nil {
		logger.Warningf("Error patching cluster: %v", err)
	}
	return err
}

func preparePatchBytesforClusterStatus(oldCluster, newCluster *clusteroperator.Cluster) ([]byte, error) {
	oldData, err := json.Marshal(oldCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oldData for cluster %s/%s: %v", oldCluster.Namespace, oldCluster.Name, err)
	}

	// Reset spec to make sure only patch for Status or ObjectMeta is generated.
	newCluster.Spec = oldCluster.Spec
	newData, err := json.Marshal(newCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal newData for cluster %s/%s: %v", newCluster.Namespace, newCluster.Name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, clusteroperator.Cluster{})
	if err != nil {
		return nil, fmt.Errorf("failed to create two way merge patch for cluster %s/%s: %v", newCluster.Namespace, newCluster.Name, err)
	}
	return patchBytes, nil
}
