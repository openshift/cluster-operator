/*
Copyright 2018 The Kubernetes Authors.

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
	"github.com/kubernetes-incubator/apiserver-builder/pkg/builders"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

// ReplaceStorageBuilder replaces the storage builder in the API group builder
// for the specified resource builder.
func ReplaceStorageBuilder(
	apiGroupBuilder *builders.APIGroupBuilder,
	builder builders.UnversionedResourceBuilder,
	replaceBuilder func(builders.StorageBuilder) builders.StorageBuilder,
) {
	for _, version := range apiGroupBuilder.Versions {
		if version.GroupVersion != capiv1.SchemeGroupVersion {
			continue
		}
		for _, kind := range version.Kinds {
			if kind.Unversioned != builder {
				continue
			}
			kind.StorageBuilder = replaceBuilder(kind.StorageBuilder)
		}
	}
}
