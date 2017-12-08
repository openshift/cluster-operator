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

// ValidateNode validates a node being created.
func ValidateNode(node *clusteroperator.Node) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeSpec(&node.Spec, field.NewPath("spec"))...)

	return allErrs
}

// validateNodeSpec validates the spec of a node.
func validateNodeSpec(spec *clusteroperator.NodeSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeType(spec.NodeType, fldPath.Child("nodeType"))...)

	return allErrs
}

// validateNodeStatus validates the status of a node.
func validateNodeStatus(status *clusteroperator.NodeStatus, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateNodeUpdate validates an update to the spec of a node.
func ValidateNodeUpdate(new *clusteroperator.Node, old *clusteroperator.Node) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeSpec(&new.Spec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateNodeStatusUpdate validates an update to the status of a node.
func ValidateNodeStatusUpdate(new *clusteroperator.Node, old *clusteroperator.Node) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeStatus(&new.Status, field.NewPath("status"))...)

	return allErrs
}
