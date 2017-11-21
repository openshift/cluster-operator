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

// ValidateHostName is the validation function for Host names.
var ValidateHostName = apivalidation.NameIsDNSSubdomain

// ValidateHost implements the validation rules for a Host.
func ValidateHost(host *boatswain.Host) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs,
		apivalidation.ValidateObjectMeta(&host.ObjectMeta,
			false, /* namespace required */
			ValidateHostName,
			field.NewPath("metadata"))...)

	allErrs = append(allErrs, validateHostSpec(&host.Spec, field.NewPath("spec"))...)
	return allErrs
}

func validateHostSpec(spec *boatswain.HostSpec, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateHostUpdate checks that when changing from an older host to a newer host is okay ?
func ValidateHostUpdate(new *boatswain.Host, old *boatswain.Host) field.ErrorList {
	return field.ErrorList{}
}

// ValidateHostStatusUpdate checks that when changing from an older host to a newer host is okay.
func ValidateHostStatusUpdate(new *boatswain.Host, old *boatswain.Host) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateHostUpdate(new, old)...)
	return allErrs
}
