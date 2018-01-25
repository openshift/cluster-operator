/*
Copyright 2016 The Kubernetes Authors.

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

package validation

import (
	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// ValidateClusterName validates the name of a cluster.
var ValidateClusterName = apivalidation.ValidateClusterName

// ValidateCluster validates a cluster being created.
func ValidateCluster(cluster *clusteroperator.Cluster) field.ErrorList {
	allErrs := field.ErrorList{}

	for _, msg := range ValidateClusterName(cluster.Name, false) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("name"), cluster.Name, msg))
	}
	allErrs = append(allErrs, validateClusterSpec(&cluster.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateClusterStatus(&cluster.Status, field.NewPath("status"))...)

	return allErrs
}

// validateClusterSpec validates the spec of a cluster.
func validateClusterSpec(spec *clusteroperator.ClusterSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	machineSetsPath := fldPath.Child("machineSets")
	versionPath := fldPath.Child("version")
	masterCount := 0
	infraCount := 0
	machineSetNames := map[string]bool{}
	for i := range spec.MachineSets {
		machineSet := spec.MachineSets[i]
		allErrs = append(allErrs, validateClusterMachineSet(&machineSet, machineSetsPath.Index(i))...)
		if machineSetNames[machineSet.Name] {
			allErrs = append(allErrs, field.Duplicate(machineSetsPath.Index(i).Child("name"), machineSet.Name))
		}
		machineSetNames[machineSet.Name] = true
		if machineSet.NodeType == clusteroperator.NodeTypeMaster {
			masterCount++
			if masterCount > 1 {
				allErrs = append(allErrs, field.Invalid(machineSetsPath.Index(i).Child("type"), machineSet.NodeType, "can only have one master machineset"))
			}
		}
		if machineSet.Infra {
			infraCount++
			if infraCount > 1 {
				allErrs = append(allErrs, field.Invalid(machineSetsPath.Index(i).Child("infra"), machineSet.Infra, "can only have one infra machineset"))
			}
		}
	}

	if masterCount == 0 {
		allErrs = append(allErrs, field.Invalid(machineSetsPath, &spec.MachineSets, "must have one machineset with a master node type"))
	}

	if infraCount == 0 {
		allErrs = append(allErrs, field.Invalid(machineSetsPath, &spec.MachineSets, "must have one machineset that hosts infra pods"))
	}

	if len(spec.ClusterVersionRef.Name) == 0 {
		allErrs = append(allErrs, field.Required(versionPath.Child("name"), "must specify a cluster version to install"))
	}

	return allErrs
}

func validateClusterMachineSet(machineSet *clusteroperator.ClusterMachineSet, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, msg := range apivalidation.NameIsDNSSubdomain(machineSet.Name, false) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), machineSet.Name, msg))
	}
	allErrs = append(allErrs, validateMachineSetConfig(&machineSet.MachineSetConfig, fldPath)...)
	return allErrs
}

// validateClusterStatus validates the status of a cluster.
func validateClusterStatus(status *clusteroperator.ClusterStatus, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if status.MachineSetCount < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("machineSetCount"), status.MachineSetCount, "must be greater than zero"))
	}
	allErrs = append(allErrs, validateSecretRef(status.AdminKubeconfig, fldPath.Child("adminKubeconfig"))...)

	return allErrs
}

func validateSecretRef(ref *corev1.LocalObjectReference, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if ref == nil {
		return allErrs
	}
	if len(ref.Name) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), "must specify a secret name"))
	}
	return allErrs
}

// ValidateClusterUpdate validates an update to the spec of a cluster.
func ValidateClusterUpdate(new *clusteroperator.Cluster, old *clusteroperator.Cluster) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateClusterSpec(&new.Spec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateClusterStatusUpdate validates an update to the status of a cluster.
func ValidateClusterStatusUpdate(new *clusteroperator.Cluster, old *clusteroperator.Cluster) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateClusterStatus(&new.Status, field.NewPath("status"))...)

	return allErrs
}
