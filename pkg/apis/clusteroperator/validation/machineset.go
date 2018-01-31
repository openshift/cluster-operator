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
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	cov1alpha1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

// ValidateMachineSet validates a machine set being created.
func ValidateMachineSet(machineSet *clusteroperator.MachineSet) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateMachineSetClusterOwner(machineSet)...)
	allErrs = append(allErrs, validateMachineSetSpec(&machineSet.Spec, field.NewPath("spec"))...)

	return allErrs
}

// validateMachineSetClusterOwner validates that a machine set has a
// controlling owner that is a cluster.
func validateMachineSetClusterOwner(machineSet *clusteroperator.MachineSet) field.ErrorList {
	allErrs := field.ErrorList{}

	ownerReferencePath := field.NewPath("metadata").Child("ownerReferences")
	controllerRef := metav1.GetControllerOf(machineSet)
	switch {
	case controllerRef == nil:
		allErrs = append(allErrs, field.Required(ownerReferencePath, "machineset must have a controlling owner"))
	case controllerRef.APIVersion != cov1alpha1.SchemeGroupVersion.String() || controllerRef.Kind != "Cluster":
		allErrs = append(allErrs, field.Invalid(ownerReferencePath, controllerRef, "controlling owner of machineset must be a cluster"))
	}

	return allErrs
}

// validateMachineSetSpec validates the spec of a machine set
func validateMachineSetSpec(spec *clusteroperator.MachineSetSpec, fldPath *field.Path) field.ErrorList {
	return validateMachineSetConfig(&spec.MachineSetConfig, fldPath)
}

// validateMachineSetConfig validates the configuration of a machine set
func validateMachineSetConfig(config *clusteroperator.MachineSetConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeType(config.NodeType, fldPath.Child("nodeType"))...)

	if config.Size <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("size"), config.Size, "size must be positive"))
	}

	return allErrs
}

// validateMachineSetStatus validates the status of a machine set.
func validateMachineSetStatus(status *clusteroperator.MachineSetStatus, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateMachineSetUpdate validates an update to the spec of a machine set.
func ValidateMachineSetUpdate(new *clusteroperator.MachineSet, old *clusteroperator.MachineSet) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateMachineSetSpec(&new.Spec, field.NewPath("spec"))...)
	allErrs = append(
		allErrs,
		apivalidation.ValidateImmutableField(
			metav1.GetControllerOf(new),
			metav1.GetControllerOf(old),
			field.NewPath("metadata").Child("ownerReferences"),
		)...,
	)

	return allErrs
}

// ValidateMachineSetStatusUpdate validates an update to the status of a machine set
func ValidateMachineSetStatusUpdate(new *clusteroperator.MachineSet, old *clusteroperator.MachineSet) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateMachineSetStatus(&new.Status, field.NewPath("status"))...)

	return allErrs
}
