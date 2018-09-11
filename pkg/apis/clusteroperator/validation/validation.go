/*
Copyright 2017 The Kubernetes Authors.

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

// validNodeTypes is a map containing an entry for every valid NodeType value.
var validNodeTypes = map[coapi.NodeType]bool{
	coapi.NodeTypeMaster:  true,
	coapi.NodeTypeCompute: true,
}

// validNodeTypeValues is an array of every valid NodeType value.
var validNodeTypeValues = func() []string {
	validValues := make([]string, len(validNodeTypes))
	i := 0
	for nodeType := range validNodeTypes {
		validValues[i] = string(nodeType)
		i++
	}
	return validValues
}()

// validateNodeType validates that the specified node type has a valid
// NodeType value.
func validateNodeType(nodeType coapi.NodeType, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if !validNodeTypes[nodeType] {
		allErrs = append(allErrs, field.NotSupported(fldPath, nodeType, validNodeTypeValues))
	}

	return allErrs
}
