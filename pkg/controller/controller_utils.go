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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

var (
	// KeyFunc returns the key identifying a cluster-operator resource.
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

	clusterKind = clusteroperator.SchemeGroupVersion.WithKind("Cluster")
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

type Condition struct {
	Status  corev1.ConditionStatus
	Reason  string
	Message string
}

type UpdateConditionCheck func(old, new Condition) bool

func UpdateConditionAlways(old, new Condition) bool {
	return true
}

func UpdateConditionNever(old, new Condition) bool {
	return false
}

func UpdateConditionIfReasonOrMessageChange(old, new Condition) bool {
	return old.Reason != new.Reason ||
		old.Message != new.Message
}

func shouldUpdateCondition(
	oldStatus corev1.ConditionStatus, oldReason, oldMessage string,
	newStatus corev1.ConditionStatus, newReason, newMessage string,
	updateConditionCheck UpdateConditionCheck,
) bool {
	if oldStatus != newStatus {
		return true
	}
	return updateConditionCheck(
		Condition{
			Status:  oldStatus,
			Reason:  oldReason,
			Message: oldMessage,
		},
		Condition{
			Status:  oldStatus,
			Reason:  oldReason,
			Message: oldMessage,
		},
	)
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

// SetClusterCondition sets the condition for the cluster.
// If the cluster does not already have a condition with the specified type,
// a condition will be added to the cluster if and only if the specified
// status is True.
// If the cluster does already have a condition with the specified type,
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

func verifyExtraMachineSetConditionChecks(
	old, new clusteroperator.MachineSetCondition,
	needsToBeUpdated ...func(old, new clusteroperator.MachineSetCondition) bool,
) bool {
	for _, check := range needsToBeUpdated {
		if check(old, new) {
			return true
		}
	}
	return false
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

// ClusterForMachineSet retrieves the cluster to which a machine set belongs.
func ClusterForMachineSet(machineSet *clusteroperator.MachineSet, clustersLister lister.ClusterLister) (*clusteroperator.Cluster, error) {
	controllerRef := metav1.GetControllerOf(machineSet)
	if controllerRef.Kind != clusterKind.Kind {
		return nil, nil
	}
	cluster, err := clustersLister.Clusters(machineSet.Namespace).Get(controllerRef.Name)
	if err != nil {
		return nil, err
	}
	if cluster.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil, nil
	}
	return cluster, nil
}
