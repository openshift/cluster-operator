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
	"bytes"
	"fmt"

	"github.com/golang/glog"

	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/cluster-operator/pkg/api"
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

var (
	// KeyFunc returns the key identifying a cluster-operator resource.
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

	clusterKind = clusteroperator.SchemeGroupVersion.WithKind("Cluster")

	// ClusterUIDLabel is the label to apply to objects that belong to the cluster
	// with the UID.
	ClusterUIDLabel = "cluster-uid"
	// ClusterNameLabel is the label to apply to objects that belong to the
	// cluster with the name.
	ClusterNameLabel = "cluster"
	// MachineSetShortNameLabel is the label to apply to machine sets, and their
	// descendants, with the short name.
	MachineSetShortNameLabel = "machine-set-short-name"
	// MachineSetUIDLabel is the label to apply to objects that belong to the
	// machine set with the UID.
	MachineSetUIDLabel = "machine-set-uid"
	// MachineSetNameLabel is the label to apply to objects that belong to the
	// machine set with the name.
	MachineSetNameLabel = "machine-set"
	// JobTypeLabel is the label to apply to jobs and configmaps that are used
	// to execute the type of job.
	JobTypeLabel = "job-type"
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
	clusterStatus *clusteroperator.ClusterStatus,
	conditionType clusteroperator.ClusterConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) {
	now := metav1.Now()
	existingCondition := FindClusterCondition(clusterStatus, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			clusterStatus.Conditions = append(
				clusterStatus.Conditions,
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
func FindClusterCondition(clusterStatus *clusteroperator.ClusterStatus, conditionType clusteroperator.ClusterConditionType) *clusteroperator.ClusterCondition {
	for i, condition := range clusterStatus.Conditions {
		if condition.Type == conditionType {
			return &clusterStatus.Conditions[i]
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

// ClusterForGenericMachineSet retrieves the cluster to which a machine set belongs.
func ClusterForGenericMachineSet(
	machineSet metav1.Object,
	clusterKind schema.GroupVersionKind,
	getCluster func(namespace, name string) (metav1.Object, error),
) (metav1.Object, error) {
	controller, err := GetObjectController(
		machineSet,
		clusterKind,
		func(name string) (metav1.Object, error) {
			return getCluster(machineSet.GetNamespace(), name)
		},
	)
	if err != nil {
		return nil, err
	}
	return controller, nil
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

// MachineSetLabels returns the labels to apply to a machine set belonging to the
// specified cluster and having the specified short name.
func MachineSetLabels(cluster *clusteroperator.Cluster, machineSetShortName string) map[string]string {
	return map[string]string{
		ClusterUIDLabel:          string(cluster.UID),
		ClusterNameLabel:         cluster.Name,
		MachineSetShortNameLabel: machineSetShortName,
	}
}

// JobLabelsForClusterController returns the labels to apply to a job doing a task
// for the specified cluster.
// The cluster parameter is a metav1.Object because it could be either a
// cluster-operator Cluster, and cluster-api Cluster, or a CombinedCluster.
func JobLabelsForClusterController(cluster metav1.Object, jobType string) map[string]string {
	return map[string]string{
		ClusterUIDLabel:  string(cluster.GetUID()),
		ClusterNameLabel: cluster.GetName(),
		JobTypeLabel:     jobType,
	}
}

// JobLabelsForMachineSetController returns the labels to apply to a job doing a
// task for the specified machine set.
func JobLabelsForMachineSetController(machineSet *clusteroperator.MachineSet, jobType string) map[string]string {
	labels := map[string]string{
		MachineSetUIDLabel:  string(machineSet.UID),
		MachineSetNameLabel: machineSet.Name,
		JobTypeLabel:        jobType,
	}
	for k, v := range machineSet.Labels {
		labels[k] = v
	}
	return labels
}

// AddLabels add the additional labels to the existing labels of the object.
func AddLabels(obj metav1.Object, additionalLabels map[string]string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
		obj.SetLabels(labels)
	}
	for k, v := range additionalLabels {
		labels[k] = v
	}
}

// ClusterSpecFromClusterAPI gets the cluster-operator ClusterSpec from the
// specified cluster-api Cluster.
func ClusterSpecFromClusterAPI(cluster *clusterapi.Cluster) (*clusteroperator.ClusterSpec, error) {
	if cluster.Spec.ProviderConfig.Value == nil {
		return nil, fmt.Errorf("No Value in ProviderConfig")
	}
	obj, _, err := api.Codecs.UniversalDecoder(clusteroperator.SchemeGroupVersion).Decode([]byte(cluster.Spec.ProviderConfig.Value.Raw), nil, nil)
	if err != nil {
		return nil, err
	}
	spec, ok := obj.(*clusteroperator.ClusterProviderConfigSpec)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", obj)
	}
	return &spec.ClusterSpec, nil
}

// ClusterStatusFromClusterAPI gets the cluster-operator ClusterStatus from the
// specified cluster-api Cluster.
func ClusterStatusFromClusterAPI(cluster *clusterapi.Cluster) (*clusteroperator.ClusterStatus, error) {
	if cluster.Status.ProviderStatus == nil {
		return &clusteroperator.ClusterStatus{}, nil
	}
	obj, _, err := api.Codecs.UniversalDecoder(clusteroperator.SchemeGroupVersion).Decode([]byte(cluster.Status.ProviderStatus.Raw), nil, nil)
	if err != nil {
		return nil, err
	}
	status, ok := obj.(*clusteroperator.ClusterProviderStatus)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", obj)
	}
	return &status.ClusterStatus, nil
}

// ClusterAPIProviderStatusFromClusterStatus gets the cluster-api ProviderStatus
// storing the cluster-operator ClusterStatus.
func ClusterAPIProviderStatusFromClusterStatus(clusterStatus *clusteroperator.ClusterStatus) (*runtime.RawExtension, error) {
	clusterProviderStatus := &clusteroperator.ClusterProviderStatus{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusteroperator.SchemeGroupVersion.String(),
			Kind:       "ClusterProviderStatus",
		},
		ClusterStatus: *clusterStatus,
	}
	serializer := json.NewSerializer(json.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	err := serializer.Encode(clusterProviderStatus, &buffer)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{
		Raw: buffer.Bytes(),
	}, nil
}
