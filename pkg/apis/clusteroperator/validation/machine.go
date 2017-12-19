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
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// ValidateMachine validates a machine being created.
func ValidateMachine(machine *clusteroperator.Machine) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateMachineSpec(&machine.Spec, field.NewPath("spec"))...)

	return allErrs
}

// validateMachineSpec validates the spec of a machine.
func validateMachineSpec(spec *clusteroperator.MachineSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeType(spec.NodeType, fldPath.Child("nodeType"))...)

	return allErrs
}

// validateMachineStatus validates the status of a machine.
func validateMachineStatus(status *clusteroperator.MachineStatus, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateMachineUpdate validates an update to the spec of a machine.
func ValidateMachineUpdate(new *clusteroperator.Machine, old *clusteroperator.Machine) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateMachineSpec(&new.Spec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateMachineStatusUpdate validates an update to the status of a machine.
func ValidateMachineStatusUpdate(new *clusteroperator.Machine, old *clusteroperator.Machine) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateMachineStatus(&new.Status, field.NewPath("status"))...)

	return allErrs
}
