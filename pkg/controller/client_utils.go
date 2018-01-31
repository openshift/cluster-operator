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

	// Do not send patch request if there is nothing to patch
	if string(patchBytes) == "{}" {
		return nil
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

func PatchMachineSetStatus(c clusteroperatorclientset.Interface, oldMachineSet, newMachineSet *clusteroperator.MachineSet) error {
	logger := log.WithField("machineset", fmt.Sprintf("%s/%s", oldMachineSet.Namespace, oldMachineSet.Name))
	patchBytes, err := preparePatchBytesForMachineSetStatus(oldMachineSet, newMachineSet)
	if err != nil {
		return err
	}

	// Do not send patch request if there is nothing to patch
	if string(patchBytes) == "{}" {
		return nil
	}

	logger.Debugf("about to patch machineset with %s", string(patchBytes))
	_, err = c.Clusteroperator().MachineSets(newMachineSet.Namespace).Patch(newMachineSet.Name, types.StrategicMergePatchType, patchBytes, "status")
	if err != nil {
		logger.Warningf("Error patching machineset: %v", err)
	}
	return err
}

func preparePatchBytesForMachineSetStatus(oldMachineSet, newMachineSet *clusteroperator.MachineSet) ([]byte, error) {
	oldData, err := json.Marshal(oldMachineSet)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oldData for machine set %s/%s: %v", oldMachineSet.Namespace, oldMachineSet.Name, err)
	}

	// Reset spec to make sure only patch for Status or ObjectMeta is generated.
	newMachineSet.Spec = oldMachineSet.Spec
	newData, err := json.Marshal(newMachineSet)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal newData for machine set %s/%s: %v", newMachineSet.Namespace, newMachineSet.Name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, clusteroperator.MachineSet{})
	if err != nil {
		return nil, fmt.Errorf("failed to create two way merge patch for machine set %s/%s: %v", newMachineSet.Namespace, newMachineSet.Name, err)
	}
	return patchBytes, nil
}
