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

// ValidateNodeGroup validates a node group being created.
func ValidateNodeGroup(nodeGroup *clusteroperator.NodeGroup) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeGroupClusterOwner(nodeGroup.GetOwnerReferences(), field.NewPath("metadata").Child("ownerReferences"))...)
	allErrs = append(allErrs, validateNodeGroupSpec(&nodeGroup.Spec, field.NewPath("spec"))...)

	return allErrs
}

// validateNodeGroupClusterOwner validates that a node group has an owner and
// that the first owner is a cluster.
func validateNodeGroupClusterOwner(ownerRefs []metav1.OwnerReference, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(ownerRefs) != 0 {
		ownerRef := ownerRefs[0]
		if ownerRef.APIVersion != cov1alpha1.SchemeGroupVersion.String() ||
			ownerRef.Kind != "clusters" {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(0), ownerRef, "first owner of nodegroup must be a cluster"))
		}
		if ownerRef.Controller == nil || !*ownerRef.Controller {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(0).Child("controller"), ownerRef.Controller, "first owner must be the managing controller"))
		}
	} else {
		allErrs = append(allErrs, field.Required(fldPath, "nodegroup must have an owner"))
	}

	return allErrs
}

// validateNodeGroupSpec validates the spec of a node group.
func validateNodeGroupSpec(spec *clusteroperator.NodeGroupSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeType(spec.NodeType, fldPath.Child("nodeType"))...)

	if spec.Size <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("size"), spec.Size, "size must be positive"))
	}

	return allErrs
}

// validateNodeGroupStatus validates the status of a node group.
func validateNodeGroupStatus(status *clusteroperator.NodeGroupStatus, fldPath *field.Path) field.ErrorList {
	return field.ErrorList{}
}

// ValidateNodeGroupUpdate validates an update to the spec of a node group.
func ValidateNodeGroupUpdate(new *clusteroperator.NodeGroup, old *clusteroperator.NodeGroup) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeGroupSpec(&new.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateNodeGroupImmutableClusterOwner(new.GetOwnerReferences(), old.GetOwnerReferences(), field.NewPath("metadata").Child("ownerReferences"))...)

	return allErrs
}

// validateNodeGroupImmutableClusterOwner validates that the cluster owner of
// a node group is immutable.
func validateNodeGroupImmutableClusterOwner(newOwnerRefs, oldOwnerRefs []metav1.OwnerReference, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(newOwnerRefs) != 0 {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newOwnerRefs[0], oldOwnerRefs[0], fldPath.Index(0))...)
	} else {
		allErrs = append(allErrs, field.Required(fldPath, "nodegroup must have an owner"))
	}

	return allErrs
}

// ValidateNodeGroupStatusUpdate validates an update to the status of a node
// group.
func ValidateNodeGroupStatusUpdate(new *clusteroperator.NodeGroup, old *clusteroperator.NodeGroup) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateNodeGroupStatus(&new.Status, field.NewPath("status"))...)

	return allErrs
}
