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

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// ValidateClusterVersion validates a cluster version being created.
func ValidateClusterVersion(cv *clusteroperator.ClusterVersion) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateClusterVersionSpec(&cv.Spec, field.NewPath("spec"))...)

	return allErrs
}

func ValidateClusterVersionUpdate(newCV *clusteroperator.ClusterVersion, oldCV *clusteroperator.ClusterVersion) field.ErrorList {
	allErrs := field.ErrorList{}
	// For now updating cluster versions is not supported. In the future this may change if deemed useful.
	// In the meantime it will be necessary to create a new cluster version and trigger an upgrade if modifications
	// are required.

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newCV.Spec, oldCV.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateClusterVersionStatusUpdate validates an update to the status of a clusterversion.
func ValidateClusterVersionStatusUpdate(new *clusteroperator.ClusterVersion, old *clusteroperator.ClusterVersion) field.ErrorList {
	allErrs := field.ErrorList{}

	// No validation required yet as the status struct is currently just a placeholder.

	return allErrs
}

func ValidateClusterVersionSpec(spec *clusteroperator.ClusterVersionSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.ImageFormat == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("imageFormat"), "must define image format"))
	}

	reposPath := fldPath.Child("yumRepositories")
	for i, r := range spec.YumRepositories {
		allErrs = append(allErrs, ValidateYumRepository(&r, reposPath.Index(i))...)
	}

	vmImagesPath := fldPath.Child("vmImages")
	if spec.VMImages == (clusteroperator.VMImages{}) {
		allErrs = append(allErrs, field.Required(vmImagesPath, "must define VM images"))
	}

	return allErrs
}

func ValidateYumRepository(repo *clusteroperator.YumRepository, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if repo.ID == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("id"), "must define id"))
	}

	if repo.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), "must define name"))
	}

	if repo.BaseURL == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("baseurl"), "must define baseurl"))
	}

	// We use an int for these settings to match yum, make sure the user doesn't specify an invalid one:
	if repo.Enabled < 0 || repo.Enabled > 1 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("enabled"), repo.Enabled, "must be 0 or 1"))
	}
	if repo.GPGCheck < 0 || repo.GPGCheck > 1 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("gpgcheck"), repo.GPGCheck, "must be 0 or 1"))
	}
	return allErrs
}
