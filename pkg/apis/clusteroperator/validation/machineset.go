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

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// validateMachineSetConfig validates the configuration of a machine set
func validateMachineSetConfig(config *coapi.MachineSetConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeType(config.NodeType, fldPath.Child("nodeType"))...)

	if config.Size <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("size"), config.Size, "size must be positive"))
	}

	return allErrs
}
