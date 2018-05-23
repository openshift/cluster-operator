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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster"

	"github.com/golang/glog"
	"github.com/openshift/cluster-operator/pkg/controller"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/builders"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	clusteroperatorValidation "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/validation"
)

// ClusterStrategy is the strategy the API server will use for Cluster resources
type ClusterStrategy struct {
	builders.StorageBuilder
}

var validateClusterName = apivalidation.ValidateClusterName

// Validate checks that an instance of Cluster is well formed
func (m ClusterStrategy) Validate(ctx request.Context, obj runtime.Object) field.ErrorList {
	errors := field.ErrorList{}
	//errors = append(errors, m.StorageBuilder.Validate(ctx, obj)...)
	cluster := obj.(*cluster.Cluster)

	for _, msg := range validateClusterName(cluster.Name, false) {
		errors = append(errors, field.Invalid(field.NewPath("metadata").Child("name"), cluster.Name, msg))
	}

	glog.Infof("Validating clusterapi.ProviderConfig/clusteroperator.ClusterSpec")
	coClusterSpec, err := controller.InternalClusterSpecFromInternalClusterAPI(cluster)
	if err != nil {
		errors = append(errors, field.Invalid(field.NewPath("providerConfig"), &cluster.Spec.ProviderConfig, err.Error()))
	} else {
		glog.Infof("REMOVE: Custom validation of Cluster %s", cluster.Name)

		validationErrors := clusteroperatorValidation.ValidateClusterSpec(coClusterSpec, field.NewPath("providerConfig"))
		errors = append(errors, validationErrors...)

		glog.Infof("REMOVE: Custom validation of Cluster complete")
	}

	return errors
}

// PrepareForCreate clears fields that are not allowed to be set by end users on creation.
func (m ClusterStrategy) PrepareForCreate(ctx request.Context, obj runtime.Object) {
	m.StorageBuilder.PrepareForCreate(ctx, obj)
}

// ReplaceClusterStorageBuilder replaces the storage builder used upstream for
// Cluster resources with a custom ClusterStrategy builder.
func ReplaceClusterStorageBuilder(storageBuilder builders.StorageBuilder) builders.StorageBuilder {
	return &ClusterStrategy{storageBuilder}
}
