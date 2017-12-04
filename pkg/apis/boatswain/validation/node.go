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
	"k8s.io/apimachinery/pkg/util/validation/field"

	boatswain "github.com/staebler/boatswain/pkg/apis/boatswain"
)

// ValidateNodeName is the validation function for Node names.
var ValidateNodeName = apivalidation.NameIsDNSSubdomain

// ValidateNode implements the validation rules for a Node.
func ValidateNode(node *boatswain.Node) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs,
		apivalidation.ValidateObjectMeta(&node.ObjectMeta,
			true, /* namespace required */
			ValidateNodeName,
			field.NewPath("metadata"))...)

	allErrs = append(allErrs, validateNodeSpec(&node.Spec, field.NewPath("spec"))...)
	return allErrs
}

func validateNodeSpec(spec *boatswain.NodeSpec, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateNodeUpdate checks that when changing from an older node to a newer node is okay ?
func ValidateNodeUpdate(new *boatswain.Node, old *boatswain.Node) field.ErrorList {
	return field.ErrorList{}
}

// ValidateNodeStatusUpdate checks that when changing from an older node to a newer node is okay.
func ValidateNodeStatusUpdate(new *boatswain.Node, old *boatswain.Node) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateNodeUpdate(new, old)...)
	return allErrs
}
