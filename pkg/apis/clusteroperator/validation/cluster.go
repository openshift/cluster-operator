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

	allErrs = append(allErrs, validateClusterNodeGroup(&spec.MasterNodeGroup, fldPath.Child("masterNodeGroup"))...)
	computeNodeGroupsFldPath := fldPath.Child("computeNodeGroup")
	for i, nodeGroup := range spec.ComputeNodeGroups {
		allErrs = append(allErrs, validateClusterComputeNodeGroup(&nodeGroup, computeNodeGroupsFldPath.Index(i))...)
	}
	computeNames := map[string]bool{}
	for i, nodeGroup := range spec.ComputeNodeGroups {
		if computeNames[nodeGroup.Name] {
			allErrs = append(allErrs, field.Duplicate(computeNodeGroupsFldPath.Index(i).Child("name"), nodeGroup.Name))
		}
		computeNames[nodeGroup.Name] = true
	}

	return allErrs
}

// validateClusterNodeGroup validates a ClusterNodeGroup in the spec of a cluster.
func validateClusterNodeGroup(nodeGroup *clusteroperator.ClusterNodeGroup, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if nodeGroup.Size <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("size"), nodeGroup.Size, "must be positive"))
	}

	return allErrs
}

// validateClusterComputeNodeGroup validates a ClusterComputeNodeGroup in the spec of a cluster.
func validateClusterComputeNodeGroup(nodeGroup *clusteroperator.ClusterComputeNodeGroup, fldPath *field.Path) field.ErrorList {
	allErrs := validateClusterNodeGroup(&nodeGroup.ClusterNodeGroup, fldPath)

	for _, msg := range apivalidation.NameIsDNSSubdomain(nodeGroup.Name, false) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), nodeGroup.Name, msg))
	}

	return allErrs
}

// validateClusterStatus validates the status of a cluster.
func validateClusterStatus(status *clusteroperator.ClusterStatus, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if status.MasterNodeGroups < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("masterNodeGroups"), status.MasterNodeGroups, "must be non-negative"))
	}
	if status.ComputeNodeGroups < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("computeNodeGroups"), status.ComputeNodeGroups, "must be non-negative"))
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
