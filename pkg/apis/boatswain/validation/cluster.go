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

// ValidateClusterName is the validation function for Cluster names.
var ValidateClusterName = apivalidation.NameIsDNSSubdomain

// ValidateCluster implements the validation rules for a Cluster.
func ValidateCluster(cluster *boatswain.Cluster) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs,
		apivalidation.ValidateObjectMeta(&cluster.ObjectMeta,
			true, /* namespace required */
			ValidateClusterName,
			field.NewPath("metadata"))...)

	allErrs = append(allErrs, validateClusterSpec(&cluster.Spec, field.NewPath("spec"))...)
	return allErrs
}

func validateClusterSpec(spec *boatswain.ClusterSpec, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateClusterUpdate checks that when changing from an older cluster to a newer cluster is okay ?
func ValidateClusterUpdate(new *boatswain.Cluster, old *boatswain.Cluster) field.ErrorList {
	return field.ErrorList{}
}

// ValidateClusterStatusUpdate checks that when changing from an older cluster to a newer cluster is okay.
func ValidateClusterStatusUpdate(new *boatswain.Cluster, old *boatswain.Cluster) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateClusterUpdate(new, old)...)
	return allErrs
}
