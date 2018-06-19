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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/cluster-operator/pkg/api"
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustoplister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capilister "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"
)

const (
	// 32 = maximum ELB name length
	// 7 = length of longest ELB name suffix ("-cp-ext")
	maxELBBasenameLen = 32 - 7

	clusterDeploymentLabel = "clusteroperator.openshift.io/cluster-deployment"

	// Domain for service names in the cluster. This is not configurable for OpenShift clusters.
	defaultClusterServiceDomain = "svc.cluster.local"

	// CIDR for service IPs. This is configured in Ansible via openshift_portal_net. However, it is not yet
	// configurable via cluster operator.
	defaultClusterServiceCIDR = "172.30.0.0/16"

	// CIDR for pod IPs. This is configured in Ansible via osm_cluster_network_cidr. However, it is not yet
	// configurable via cluster operator.
	defaultClusterPodCIDR = "10.128.0.0/14"
)

var (
	// KeyFunc returns the key identifying a cluster-operator resource.
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

	// ClusterDeploymentKind is the GVK for a ClusterDeployment.
	ClusterDeploymentKind = clusteroperator.SchemeGroupVersion.WithKind("ClusterDeployment")

	// Domain for service names in the cluster. This is not configurable for OpenShift clusters.
	defaultServiceDomain = "svc.cluster.local"

	// CIDR for service IPs. This is configured in Ansible via openshift_portal_net. However, it is not yet
	// configurable via cluster operator.
	defaultServiceCIDR = "172.30.0.0/16"

	// CIDR for pod IPs. This is configured in Ansible via osm_cluster_network_cidr. However, it is not yet
	// configurable via cluster operator.
	defaultPodCIDR = "10.128.0.0/14"

	clusterKind = clusterapi.SchemeGroupVersion.WithKind("Cluster")

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
	clusterStatus *clusteroperator.ClusterDeploymentStatus,
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
func FindClusterCondition(clusterStatus *clusteroperator.ClusterDeploymentStatus, conditionType clusteroperator.ClusterConditionType) *clusteroperator.ClusterCondition {
	for i, condition := range clusterStatus.Conditions {
		if condition.Type == conditionType {
			return &clusterStatus.Conditions[i]
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
func ClusterForMachineSet(machineSet *clusterapi.MachineSet, clusterLister capilister.ClusterLister) (*clusterapi.Cluster, error) {
	if machineSet.Labels == nil {
		return nil, fmt.Errorf("missing %s label", clusteroperator.ClusterNameLabel)
	}
	clusterName, ok := machineSet.Labels[clusteroperator.ClusterNameLabel]
	if !ok {
		return nil, fmt.Errorf("missing %s label", clusteroperator.ClusterNameLabel)
	}
	return clusterLister.Clusters(machineSet.Namespace).Get(clusterName)
}

// MachineSetsForCluster retrieves the machinesets associated with the
// specified cluster name.
func MachineSetsForCluster(namespace string, clusterName string, machineSetsLister capilister.MachineSetLister) ([]*clusterapi.MachineSet, error) {
	requirement, err := labels.NewRequirement(clusteroperator.ClusterNameLabel, selection.Equals, []string{clusterName})
	if err != nil {
		return nil, err
	}
	return machineSetsLister.MachineSets(namespace).List(labels.NewSelector().Add(*requirement))
}

// ClusterDeploymentForCluster retrieves the cluster deployment that owns the cluster.
func ClusterDeploymentForCluster(cluster *clusterapi.Cluster, clusterDeploymentsLister clustoplister.ClusterDeploymentLister) (*clusteroperator.ClusterDeployment, error) {
	controller, err := GetObjectController(
		cluster,
		ClusterDeploymentKind,
		func(name string) (metav1.Object, error) {
			return clusterDeploymentsLister.ClusterDeployments(cluster.Namespace).Get(name)
		},
	)
	if err != nil {
		return nil, err
	}
	if controller == nil {
		return nil, nil
	}
	clusterDeployment, ok := controller.(*clusteroperator.ClusterDeployment)
	if !ok {
		return nil, fmt.Errorf("Could not convert controller into a ClusterDeployment")
	}
	return clusterDeployment, nil
}

// ClusterDeploymentForMachineSet retrieves the cluster deployment that owns the machine set.
func ClusterDeploymentForMachineSet(machineSet *clusterapi.MachineSet, clusterDeploymentsLister clustoplister.ClusterDeploymentLister) (*clusteroperator.ClusterDeployment, error) {
	controller, err := GetObjectController(
		machineSet,
		ClusterDeploymentKind,
		func(name string) (metav1.Object, error) {
			return clusterDeploymentsLister.ClusterDeployments(machineSet.Namespace).Get(name)
		},
	)
	if err != nil {
		return nil, err
	}
	if controller == nil {
		return nil, nil
	}
	clusterDeployment, ok := controller.(*clusteroperator.ClusterDeployment)
	if !ok {
		return nil, fmt.Errorf("Could not convert controller into a ClusterDeployment")
	}
	return clusterDeployment, nil
}

// MachineSetLabels returns the labels to apply to a machine set belonging to the
// specified cluster deployment and having the specified short name.
func MachineSetLabels(clusterDeployment *clusteroperator.ClusterDeployment, machineSetShortName string) map[string]string {
	return map[string]string{
		ClusterUIDLabel:          string(clusterDeployment.UID),
		ClusterNameLabel:         clusterDeployment.Name,
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

// ClusterDeploymentSpecFromCluster gets the cluster-operator ClusterDeploymentSpec from the
// specified cluster-api Cluster.
func ClusterDeploymentSpecFromCluster(cluster *clusterapi.Cluster) (*clusteroperator.ClusterDeploymentSpec, error) {
	if cluster.Spec.ProviderConfig.Value == nil {
		return nil, fmt.Errorf("No Value in ProviderConfig")
	}
	obj, gvk, err := api.Codecs.UniversalDecoder(clusteroperator.SchemeGroupVersion).Decode([]byte(cluster.Spec.ProviderConfig.Value.Raw), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("could not decode ProviderConfig: %v", err)
	}
	spec, ok := obj.(*clusteroperator.ClusterProviderConfigSpec)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", gvk)
	}
	return &spec.ClusterDeploymentSpec, nil
}

// ClusterStatusFromClusterAPI gets the cluster-operator ClusterStatus from the
// specified cluster-api Cluster.
func ClusterStatusFromClusterAPI(cluster *clusterapi.Cluster) (*clusteroperator.ClusterDeploymentStatus, error) {
	if cluster.Status.ProviderStatus == nil {
		return &clusteroperator.ClusterDeploymentStatus{}, nil
	}
	obj, gvk, err := api.Codecs.UniversalDecoder(clusteroperator.SchemeGroupVersion).Decode([]byte(cluster.Status.ProviderStatus.Raw), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("could not decode ProviderStatus: %v", err)
	}
	status, ok := obj.(*clusteroperator.ClusterProviderStatus)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", gvk)
	}
	return &status.ClusterDeploymentStatus, nil
}

// ClusterProviderConfigSpecFromClusterDeploymentSpec returns an encoded RawExtension with a ClusterProviderConfigSpec from a cluster deployment spec
func ClusterProviderConfigSpecFromClusterDeploymentSpec(clusterDeploymentSpec *clusteroperator.ClusterDeploymentSpec) (*runtime.RawExtension, error) {
	clusterProviderConfigSpec := &clusteroperator.ClusterProviderConfigSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusteroperator.SchemeGroupVersion.String(),
			Kind:       "ClusterProviderConfigSpec",
		},
		ClusterDeploymentSpec: *clusterDeploymentSpec,
	}
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	err := serializer.Encode(clusterProviderConfigSpec, &buffer)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{
		Raw: buffer.Bytes(),
	}, nil
}

// ClusterAPIProviderStatusFromClusterStatus gets the cluster-api ProviderStatus
// storing the cluster-operator ClusterStatus.
func ClusterAPIProviderStatusFromClusterStatus(clusterStatus *clusteroperator.ClusterDeploymentStatus) (*runtime.RawExtension, error) {
	clusterProviderStatus := &clusteroperator.ClusterProviderStatus{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusteroperator.SchemeGroupVersion.String(),
			Kind:       "ClusterProviderStatus",
		},
		ClusterDeploymentStatus: *clusterStatus,
	}
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	err := serializer.Encode(clusterProviderStatus, &buffer)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{
		Raw: bytes.TrimSpace(buffer.Bytes()),
	}, nil
}

// MachineSetSpecFromClusterAPIMachineSpec gets the cluster-operator MachineSetSpec from the
// specified cluster-api MachineSet.
func MachineSetSpecFromClusterAPIMachineSpec(ms *clusterapi.MachineSpec) (*clusteroperator.MachineSetSpec, error) {
	if ms.ProviderConfig.Value == nil {
		return nil, fmt.Errorf("No Value in ProviderConfig")
	}
	obj, gvk, err := api.Codecs.UniversalDecoder(clusteroperator.SchemeGroupVersion).Decode([]byte(ms.ProviderConfig.Value.Raw), nil, nil)
	if err != nil {
		return nil, err
	}
	spec, ok := obj.(*clusteroperator.MachineSetProviderConfigSpec)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", gvk)
	}
	return &spec.MachineSetSpec, nil
}

// BuildCluster builds a cluster for the given cluster deployment.
func BuildCluster(clusterDeployment *clusteroperator.ClusterDeployment) (*clusterapi.Cluster, error) {
	cluster := &clusterapi.Cluster{}
	cluster.Name = clusterDeployment.Spec.ClusterID
	cluster.Labels = clusterDeployment.Labels
	cluster.Namespace = clusterDeployment.Namespace
	if cluster.Labels == nil {
		cluster.Labels = make(map[string]string)
	}
	cluster.Labels[clusteroperator.ClusterDeploymentLabel] = clusterDeployment.Name
	blockOwnerDeletion := false
	controllerRef := metav1.NewControllerRef(clusterDeployment, ClusterDeploymentKind)
	controllerRef.BlockOwnerDeletion = &blockOwnerDeletion
	cluster.OwnerReferences = []metav1.OwnerReference{*controllerRef}
	providerConfig, err := ClusterProviderConfigSpecFromClusterDeploymentSpec(&clusterDeployment.Spec)
	if err != nil {
		return nil, err
	}
	cluster.Spec.ProviderConfig.Value = providerConfig

	// Set networking defaults
	cluster.Spec.ClusterNetwork.ServiceDomain = defaultServiceDomain
	cluster.Spec.ClusterNetwork.Pods.CIDRBlocks = []string{defaultPodCIDR}
	cluster.Spec.ClusterNetwork.Services.CIDRBlocks = []string{defaultServiceCIDR}
	return cluster, nil
}

// ClusterAPIMachineProviderConfigFromMachineSetSpec gets the cluster-api ProviderConfig for a Machine template
// to store the cluster-operator MachineSetSpec.
func ClusterAPIMachineProviderConfigFromMachineSetSpec(machineSetSpec *clusteroperator.MachineSetSpec) (*runtime.RawExtension, error) {
	msProviderConfigSpec := &clusteroperator.MachineSetProviderConfigSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusteroperator.SchemeGroupVersion.String(),
			Kind:       "MachineSetProviderConfigSpec",
		},
		MachineSetSpec: *machineSetSpec,
	}
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	err := serializer.Encode(msProviderConfigSpec, &buffer)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{
		Raw: bytes.TrimSpace(buffer.Bytes()),
	}, nil
}

// AWSMachineProviderStatusFromClusterAPIMachine gets the cluster-operator MachineSetSpec from the
// specified cluster-api MachineSet.
func AWSMachineProviderStatusFromClusterAPIMachine(m *clusterapi.Machine) (*clusteroperator.AWSMachineProviderStatus, error) {
	if m.Status.ProviderStatus == nil {
		return &clusteroperator.AWSMachineProviderStatus{}, nil
	}
	obj, gvk, err := api.Codecs.UniversalDecoder(clusteroperator.SchemeGroupVersion).Decode([]byte(m.Status.ProviderStatus.Raw), nil, nil)
	if err != nil {
		return nil, err
	}
	status, ok := obj.(*clusteroperator.AWSMachineProviderStatus)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", gvk)
	}
	return status, nil
}

// ClusterAPIMachineProviderStatusFromAWSMachineProviderStatus gets the cluster-api ProviderConfig for a Machine template
// to store the cluster-operator MachineSetSpec.
func ClusterAPIMachineProviderStatusFromAWSMachineProviderStatus(awsStatus *clusteroperator.AWSMachineProviderStatus) (*runtime.RawExtension, error) {
	awsStatus.TypeMeta = metav1.TypeMeta{
		APIVersion: clusteroperator.SchemeGroupVersion.String(),
		Kind:       "AWSMachineProviderStatus",
	}
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	err := serializer.Encode(awsStatus, &buffer)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{
		Raw: bytes.TrimSpace(buffer.Bytes()),
	}, nil
}

// MachineProviderConfigFromMachineSetConfig returns a RawExtension with a machine ProviderConfig from a MachineSetConfig
func MachineProviderConfigFromMachineSetConfig(machineSetConfig *clusteroperator.MachineSetConfig, clusterDeploymentSpec *clusteroperator.ClusterDeploymentSpec, clusterVersion *clusteroperator.ClusterVersion) (*runtime.RawExtension, error) {
	msSpec := &clusteroperator.MachineSetSpec{
		ClusterID:        clusterDeploymentSpec.ClusterID,
		MachineSetConfig: *machineSetConfig,
	}
	vmImage, err := getImage(clusterDeploymentSpec, clusterVersion)
	if err != nil {
		return nil, err
	}
	msSpec.VMImage = *vmImage

	// use cluster defaults for hardware spec if unset:
	hwSpec, err := ApplyDefaultMachineSetHardwareSpec(msSpec.Hardware, clusterDeploymentSpec.DefaultHardwareSpec)
	if err != nil {
		return nil, err
	}
	msSpec.Hardware = hwSpec

	// Copy cluster hardware onto the provider config as well. Needed when deleting a cluster in the actuator.
	msSpec.ClusterHardware = clusterDeploymentSpec.Hardware

	return ClusterAPIMachineProviderConfigFromMachineSetSpec(msSpec)
}

// getImage returns a specific image for the given machine and cluster version.
func getImage(clusterSpec *clusteroperator.ClusterDeploymentSpec, clusterVersion *clusteroperator.ClusterVersion) (*clusteroperator.VMImage, error) {
	if clusterSpec.Hardware.AWS == nil {
		return nil, fmt.Errorf("no AWS hardware defined for cluster")
	}

	if clusterVersion.Spec.VMImages.AWSImages == nil {
		return nil, fmt.Errorf("no AWS images defined for cluster version")
	}

	for _, regionAMI := range clusterVersion.Spec.VMImages.AWSImages.RegionAMIs {
		if regionAMI.Region == clusterSpec.Hardware.AWS.Region {
			ami := regionAMI.AMI
			return &clusteroperator.VMImage{
				AWSImage: &ami,
			}, nil
		}
	}

	return nil, fmt.Errorf("no AWS image defined for region %s", clusterSpec.Hardware.AWS.Region)
}

// ApplyDefaultMachineSetHardwareSpec merges the cluster-wide hardware defaults with the machineset specific hardware specified.
func ApplyDefaultMachineSetHardwareSpec(machineSetHardwareSpec, defaultHardwareSpec *clusteroperator.MachineSetHardwareSpec) (*clusteroperator.MachineSetHardwareSpec, error) {
	if defaultHardwareSpec == nil {
		return machineSetHardwareSpec, nil
	}
	defaultHwSpecJSON, err := json.Marshal(defaultHardwareSpec)
	if err != nil {
		return nil, err
	}
	specificHwSpecJSON, err := json.Marshal(machineSetHardwareSpec)
	if err != nil {
		return nil, err
	}
	merged, err := strategicpatch.StrategicMergePatch(defaultHwSpecJSON, specificHwSpecJSON, machineSetHardwareSpec)
	mergedSpec := &clusteroperator.MachineSetHardwareSpec{}
	if err = json.Unmarshal(merged, mergedSpec); err != nil {
		return nil, err
	}
	return mergedSpec, nil
}

// GetInfraSize gets the size of the infra machine set for the cluster.
func GetInfraSize(cluster *clusteroperator.CombinedCluster) (int, error) {
	for _, ms := range cluster.ClusterDeploymentSpec.MachineSets {
		if ms.Infra {
			return ms.Size, nil
		}
	}
	return 0, fmt.Errorf("no machineset of type Infra found")
}

// GetMasterMachineSet gets the master machine set for the cluster.
func GetMasterMachineSet(cluster *clusteroperator.CombinedCluster, machineSetLister capilister.MachineSetLister) (*clusterapi.MachineSet, error) {
	machineSets, err := MachineSetsForCluster(cluster.Namespace, cluster.Name, machineSetLister)
	if err != nil {
		return nil, err
	}
	for _, ms := range machineSets {
		for _, role := range ms.Spec.Template.Spec.Roles {
			if role == capicommon.MasterRole {
				return ms, nil
			}
		}
	}
	return nil, fmt.Errorf("no master machineset found")
}

// MachineHasRole returns true if the machine has the given cluster-api role.
func MachineHasRole(machine *clusterapi.Machine, role capicommon.MachineRole) bool {
	for _, r := range machine.Spec.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// ELBMasterExternalName gets the name of the external master ELB for the cluster
// with the specified cluster ID.
func ELBMasterExternalName(clusterID string) string {
	return trimForELBBasename(clusterID, maxELBBasenameLen) + "-cp-ext"
}

// ELBMasterInternalName gets the name of the internal master ELB for the cluster
// with the specified cluster ID.
func ELBMasterInternalName(clusterID string) string {
	return trimForELBBasename(clusterID, maxELBBasenameLen) + "-cp-int"
}

// ELBInfraName gets the name of the infra ELB for the cluster
// with the specified cluster ID.
func ELBInfraName(clusterID string) string {
	return trimForELBBasename(clusterID, maxELBBasenameLen) + "-infra"
}

// trimForELBBasename takes a string and trims it so that it can be used as the
// basename for an ELB.
func trimForELBBasename(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	lastHyphen := strings.LastIndexByte(s, '-')
	if lastHyphen < 0 {
		return s[:maxLen]
	}
	suffix := s[lastHyphen+1:]
	if len(suffix) > maxLen {
		return suffix[:maxLen]
	}
	if len(suffix)+1 >= maxLen {
		return suffix
	}
	return s[:maxLen-len(suffix)-1] + "-" + suffix
}

// BuildClusterAPIMachineSet returns a clusterapi.MachineSet from the combination of various clusteroperator
// objects (ClusterMachineSet/ClusterDeploymentSpec/ClusterVersion) in the provided 'namespace'
func BuildClusterAPIMachineSet(ms *clusteroperator.ClusterMachineSet, clusterDeploymentSpec *clusteroperator.ClusterDeploymentSpec, clusterVersion *clusteroperator.ClusterVersion, namespace string) (*clusterapi.MachineSet, error) {
	machineSetName := fmt.Sprintf("%s-%s", clusterDeploymentSpec.ClusterID, ms.ShortName)
	capiMachineSet := clusterapi.MachineSet{}
	capiMachineSet.Name = machineSetName
	capiMachineSet.Namespace = namespace
	replicas := int32(ms.Size)
	capiMachineSet.Spec.Replicas = &replicas
	labels := map[string]string{
		"machineset": machineSetName,
		"cluster":    clusterDeploymentSpec.ClusterID,
	}
	capiMachineSet.Labels = labels
	capiMachineSet.Spec.Selector.MatchLabels = labels

	machineTemplate := clusterapi.MachineTemplateSpec{}
	machineTemplate.Labels = labels
	machineTemplate.Spec.Labels = labels

	capiMachineSet.Spec.Template = machineTemplate

	providerConfig, err := MachineProviderConfigFromMachineSetConfig(&ms.MachineSetConfig, clusterDeploymentSpec, clusterVersion)
	if err != nil {
		return nil, err
	}
	capiMachineSet.Spec.Template.Spec.ProviderConfig.Value = providerConfig

	return &capiMachineSet, nil
}

// StringPtrsEqual safely returns true if the value for each string pointer is equal, or both are nil.
func StringPtrsEqual(s1, s2 *string) bool {
	if s1 == s2 {
		return true
	}
	if s1 == nil || s2 == nil {
		return false
	}
	return *s1 == *s2
}

// HasFinalizer returns true if the given object has the given finalizer
func HasFinalizer(object metav1.Object, finalizer string) bool {
	for _, f := range object.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

// AddFinalizer adds a finalizer to the given object
func AddFinalizer(object metav1.Object, finalizer string) {
	finalizers := sets.NewString(object.GetFinalizers()...)
	finalizers.Insert(finalizer)
	object.SetFinalizers(finalizers.List())
}

// DeleteFinalizer removes a finalizer from the given object
func DeleteFinalizer(object metav1.Object, finalizer string) {
	finalizers := sets.NewString(object.GetFinalizers()...)
	finalizers.Delete(finalizer)
	object.SetFinalizers(finalizers.List())
}
