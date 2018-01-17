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

type UpdateConditionCheck func(oldReason, oldMessage, newReason, newMessage string) bool

func verifyUpdateConditionChecks(
	oldReason, oldMessage, newReason, newMessage string,
	updateConditionChecks ...UpdateConditionCheck,
) bool {
	for _, check := range updateConditionChecks {
		if check(oldReason, oldMessage, newReason, newMessage) {
			return true
		}
	}
	return false
}

// SetClusterCondition ensures that the specified cluster has a condition
// with the specified condition type and status. If there is not a condition
// with the condition type, then one is added. Otherwise, the
// existing condition is modified if any of the following are true.
// 1) Requested status is True.
// 2) Requested status is different than existing status.
// 3) Any of the updateConditionChecks checks return true.
func SetClusterCondition(
	cluster *clusteroperator.Cluster,
	conditionType clusteroperator.ClusterConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionChecks ...UpdateConditionCheck,
) {
	now := metav1.Now()
	condition := clusteroperator.ClusterCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		LastProbeTime:      now,
	}
	existingCondition := FindClusterCondition(cluster, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			cluster.Status.Conditions = append(cluster.Status.Conditions, condition)
		}
	} else {
		if status != existingCondition.Status ||
			status == corev1.ConditionTrue ||
			verifyUpdateConditionChecks(
				existingCondition.Reason,
				existingCondition.Message,
				condition.Reason,
				condition.Message,
				updateConditionChecks...,
			) {
			if existingCondition.Status != condition.Status {
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

// SetMachineSetCondition ensures that the specified machine set has a
// condition with the specified condition type and status. If there is not a
// condition with the condition type, then one is added. Otherwise, the
// existing condition is modified if any of the following are true.
// 1) Requested status is True.
// 2) Requested status is different than existing status.
// 3) Any of the updateConditionChecks checks return true.
func SetMachineSetCondition(
	machineSet *clusteroperator.MachineSet,
	conditionType clusteroperator.MachineSetConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionChecks ...UpdateConditionCheck,
) {
	now := metav1.Now()
	condition := clusteroperator.MachineSetCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		LastProbeTime:      now,
	}
	existingCondition := FindMachineSetCondition(machineSet, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			machineSet.Status.Conditions = append(machineSet.Status.Conditions, condition)
		}
	} else {
		if status != existingCondition.Status ||
			status == corev1.ConditionTrue ||
			verifyUpdateConditionChecks(
				existingCondition.Reason,
				existingCondition.Message,
				condition.Reason,
				condition.Message,
				updateConditionChecks...,
			) {
			if existingCondition.Status != condition.Status {
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
