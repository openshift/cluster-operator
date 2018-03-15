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

package controller

import (
	"fmt"

	"github.com/golang/glog"

	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

var (
	// KeyFunc returns the key identifying a cluster-operator resource.
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

	clusterKind = clusteroperator.SchemeGroupVersion.WithKind("Cluster")
	machineSetKind = clusteroperator.SchemeGroupVersion.WithKind("MachineSet")
)

// WaitForCacheSync is a wrapper around cache.WaitForCacheSync that generates log messages
// indicating that the controller identified by controllerName is waiting for syncs, followed by
// either a successful or failed sync.
func WaitForCacheSync(controllerName string, stopCh <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool {
	glog.Infof("Waiting for caches to sync for %s controller", controllerName)

	if !cache.WaitForCacheSync(stopCh, cacheSyncs...) {
		utilruntime.HandleError(fmt.Errorf("Unable to sync caches for %s controller", controllerName))
		return false
	}

	glog.Infof("Caches are synced for %s controller", controllerName)
	return true
}

// UpdateConditionCheck tests whether a condition should be updated from the
// old condition to the new condition. Returns true if the condition should
// be updated.
type UpdateConditionCheck func(oldReason, oldMessage, newReason, newMessage string) bool

// UpdateConditionAlways returns true. The condition will always be updated.
func UpdateConditionAlways(_, _, _, _ string) bool {
	return true
}

// UpdateConditionNever return false. The condition will never be updated,
// unless there is a change in the status of the condition.
func UpdateConditionNever(_, _, _, _ string) bool {
	return false
}

// UpdateConditionIfReasonOrMessageChange returns true if there is a change
// in the reason or the message of the condition.
func UpdateConditionIfReasonOrMessageChange(oldReason, oldMessage, newReason, newMessage string) bool {
	return oldReason != newReason ||
		oldMessage != newMessage
}

func shouldUpdateCondition(
	oldStatus corev1.ConditionStatus, oldReason, oldMessage string,
	newStatus corev1.ConditionStatus, newReason, newMessage string,
	updateConditionCheck UpdateConditionCheck,
) bool {
	if oldStatus != newStatus {
		return true
	}
	return updateConditionCheck(oldReason, oldMessage, newReason, newMessage)
}

// SetClusterCondition sets the condition for the cluster.
// If the cluster does not already have a condition with the specified type,
// a condition will be added to the cluster if and only if the specified
// status is True.
// If the cluster does already have a condition with the specified type,
// the condition will be updated if either of the following are true.
// 1) Requested status is different than existing status.
// 2) The updateConditionCheck function returns true.
func SetClusterCondition(
	cluster *clusteroperator.Cluster,
	conditionType clusteroperator.ClusterConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) {
	now := metav1.Now()
	existingCondition := FindClusterCondition(cluster, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			cluster.Status.Conditions = append(
				cluster.Status.Conditions,
				clusteroperator.ClusterCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}
}

// FindClusterCondition finds in the cluster the condition that has the
// specified condition type. If none exists, then returns nil.
func FindClusterCondition(cluster *clusteroperator.Cluster, conditionType clusteroperator.ClusterConditionType) *clusteroperator.ClusterCondition {
	for i, condition := range cluster.Status.Conditions {
		if condition.Type == conditionType {
			return &cluster.Status.Conditions[i]
		}
	}
	return nil
}

// SetMachineSetCondition sets the condition for the machine set.
// If the machine set does not already have a condition with the specified
// type, a condition will be added to the machine set if and only if the
// specified status is True.
// If the machine set does already have a condition with the specified type,
// the condition will be updated if either of the following are true.
// 1) Requested status is different than existing status.
// 2) The updateConditionCheck function returns true.
func SetMachineSetCondition(
	machineSet *clusteroperator.MachineSet,
	conditionType clusteroperator.MachineSetConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) {
	now := metav1.Now()
	existingCondition := FindMachineSetCondition(machineSet, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			machineSet.Status.Conditions = append(
				machineSet.Status.Conditions,
				clusteroperator.MachineSetCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}
}

// FindMachineSetCondition finds in the machine set the condition that has the
// specified condition type. If none exists, then returns nil.
func FindMachineSetCondition(machineSet *clusteroperator.MachineSet, conditionType clusteroperator.MachineSetConditionType) *clusteroperator.MachineSetCondition {
	for i, condition := range machineSet.Status.Conditions {
		if condition.Type == conditionType {
			return &machineSet.Status.Conditions[i]
		}
	}
	return nil
}

// GetObjectController get the controlling owner for the specified object.
// If there is no controlling owner or the controller owner does not have
// the specified kind, then returns nil for the controller.
func GetObjectController(
	obj metav1.Object,
	controllerKind schema.GroupVersionKind,
	getController func(name string) (metav1.Object, error),
) (metav1.Object, error) {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef == nil {
		return nil, nil
	}
	if apiVersion, kind := controllerKind.ToAPIVersionAndKind(); controllerRef.APIVersion != apiVersion ||
		controllerRef.Kind != kind {
		return nil, nil
	}
	controller, err := getController(controllerRef.Name)
	if err != nil {
		return nil, err
	}
	if controller == nil {
		return nil, nil
	}
	if controller.GetUID() != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil, nil
	}
	return controller, nil
}

// ClusterForMachineSet retrieves the cluster to which a machine set belongs.
func ClusterForMachineSet(machineSet *clusteroperator.MachineSet, clustersLister lister.ClusterLister) (*clusteroperator.Cluster, error) {
	controller, err := GetObjectController(
		machineSet,
		clusterKind,
		func(name string) (metav1.Object, error) {
			return clustersLister.Clusters(machineSet.Namespace).Get(name)
		},
	)
	if err != nil {
		return nil, err
	}
	if controller == nil {
		return nil, nil
	}
	cluster, ok := controller.(*clusteroperator.Cluster)
	if !ok {
		return nil, fmt.Errorf("Could not convert controller into a Cluster")
	}
	return cluster, nil
}

// MachineSetsForCluster retrieves the machinesets owned by a given cluster.
func MachineSetsForCluster(cluster *clusteroperator.Cluster, machineSetsLister lister.MachineSetLister) ([]*clusteroperator.MachineSet, error) {
	clusterMachineSets := []*clusteroperator.MachineSet{}
	allMachineSets, err := machineSetsLister.MachineSets(cluster.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, machineSet := range allMachineSets {
		if metav1.IsControlledBy(machineSet, cluster) {
			clusterMachineSets = append(clusterMachineSets, machineSet)
		}
	}
	return clusterMachineSets, nil
}

// MachinesForMachineSet returns list of all machines belonging to the given machineSet
func MachinesForMachineSet(ms *clusteroperator.MachineSet, machineLister lister.MachineLister) ([]*clusteroperator.Machine, error) {
	machines := []*clusteroperator.Machine{}
	allMachines, err := machineLister.Machines(ms.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, machine := range allMachines {
		if metav1.IsControlledBy(machine, ms) {
			machines = append(machines, machine)
		}
	}
	return machines, nil
}

// MachineSetForMachine retrieves the machineset to which a machine belongs
func MachineSetForMachine(machine *clusteroperator.Machine, machineSetsLister lister.MachineSetLister) (*clusteroperator.MachineSet, error) {
        controller, err := GetObjectController(
                machine,
                machineSetKind,
                func(name string) (metav1.Object, error) {
                        return machineSetsLister.MachineSets(machine.Namespace).Get(name)
                },
        )
        if err != nil {
                return nil, err
        }
        if controller == nil {
                return nil, nil
        }
        machineSet, ok := controller.(*clusteroperator.MachineSet)
        if !ok {
                return nil, fmt.Errorf("Could not convert controller into a MachineSet")
        }
        return machineSet, nil
}
