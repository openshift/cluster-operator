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
	"regexp"

	kapi "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// validDeploymentTypes is a map containing an entry for every valid DeploymentType value.
var validDeploymentTypes = map[clusteroperator.ClusterDeploymentType]bool{
	clusteroperator.ClusterDeploymentTypeOrigin:     true,
	clusteroperator.ClusterDeploymentTypeEnterprise: true,
}

// validDeploymentTypeValues is an array of every valid DeploymentType value.
var validDeploymentTypeValues = func() []string {
	validValues := make([]string, len(validDeploymentTypes))
	i := 0
	for dt := range validDeploymentTypes {
		validValues[i] = string(dt)
		i++
	}
	return validValues
}()

var versionFmt = "^v?\\d+\\.\\d+(\\..+)?$"
var versionRegex = regexp.MustCompile(versionFmt)

// validPullPolicies is a map containing an entry for every valid pull policy value.
var validPullPolicies = map[kapi.PullPolicy]bool{
	kapi.PullAlways:       true,
	kapi.PullNever:        true,
	kapi.PullIfNotPresent: true,
}

// validPullPolicyValues is an array of every valid pull policy value.
var validPullPolicyValues = func() []string {
	validValues := make([]string, len(validPullPolicies))
	i := 0
	for dt := range validPullPolicies {
		validValues[i] = string(dt)
		i++
	}
	return validValues
}()

// ValidateClusterVersion validates a cluster version being created.
func ValidateClusterVersion(cv *clusteroperator.ClusterVersion) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateClusterVersionSpec(&cv.Spec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateClusterVersionUpdate validates that a spec update of a clusterversion.
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

// ValidateClusterVersionSpec validates the spec of a ClusterVersion.
func ValidateClusterVersionSpec(spec *clusteroperator.ClusterVersionSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.Images.ImageFormat == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("imageFormat"), "must define image format"))
	}

	vmImagesPath := fldPath.Child("vmImages")
	allErrs = append(allErrs, ValidateVMImages(spec.VMImages, vmImagesPath)...)

	deploymentTypePath := fldPath.Child("deploymentType")
	if spec.DeploymentType == "" {
		allErrs = append(allErrs, field.Required(deploymentTypePath, "must define deployment type"))
	} else if !validDeploymentTypes[spec.DeploymentType] {
		allErrs = append(allErrs, field.NotSupported(deploymentTypePath, spec.DeploymentType, validDeploymentTypeValues))
	}

	versionPath := fldPath.Child("version")
	if spec.Version == "" {
		allErrs = append(allErrs, field.Required(versionPath, "must define version"))
	} else {
		matched := versionRegex.MatchString(spec.Version)
		if !matched {
			allErrs = append(allErrs, field.Invalid(versionPath, spec.Version, validation.RegexError("must begin with a valid major release version", versionFmt, "v3.9.0", "v3.9.0-alpha.4+e7d9503-189")))
		}
	}

	if spec.Images.OpenshiftAnsibleImagePullPolicy != nil {
		if !validPullPolicies[*spec.Images.OpenshiftAnsibleImagePullPolicy] {
			allErrs = append(allErrs, field.NotSupported(fldPath.Child("openshiftAnsibleImagePullPolicy"), spec.Images.OpenshiftAnsibleImagePullPolicy, validPullPolicyValues))
		}
	}

	return allErrs
}

// ValidateVMImages validates the provided image data for each cloud provider.
func ValidateVMImages(vmImages clusteroperator.VMImages, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// This can be dropped when additional providers are supported, but we should then verify
	// at least one cloud provider has images specified.
	if vmImages.AWSImages == nil {

		allErrs = append(allErrs, field.Required(fldPath.Child("awsVMImages"), "must define AWS VM images"))
		return allErrs
	}

	regionsSeen := map[string]bool{}
	amisPath := fldPath.Child("awsVMImages").Child("regionAMIs")
	if len(vmImages.AWSImages.RegionAMIs) == 0 {
		allErrs = append(allErrs, field.Required(amisPath, "must define AMIs for at least one region"))
	} else {
		for i, ami := range vmImages.AWSImages.RegionAMIs {
			allErrs = append(allErrs, ValidateRegionAMIs(&ami, amisPath.Index(i))...)
			if _, previouslySeen := regionsSeen[ami.Region]; previouslySeen {
				allErrs = append(allErrs, field.Duplicate(amisPath.Index(i).Child("region"), ami.Region))
			} else {
				regionsSeen[ami.Region] = true
			}
		}
	}

	return allErrs
}

// ValidateRegionAMIs validates that an AMI is properly defined.
func ValidateRegionAMIs(regionAMIs *clusteroperator.AWSRegionAMIs, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if regionAMIs.Region == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("region"), "must define region"))
	}

	if regionAMIs.AMI == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("ami"), "must define AMI ID"))
	}

	if regionAMIs.MasterAMI != nil && *regionAMIs.MasterAMI == "" {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("masterAMI"), regionAMIs.MasterAMI, "cannot define an empty master AMI ID"))
	}

	return allErrs
}
