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

package clusterdeployment

import (
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
)

// NewStorage creates a new rest.Storage responsible for accessing
// Cluster resources
func NewStorage(opts generic.RESTOptions) (clusters *registry.Store, clustersStatus *StatusREST) {
	store := registry.Store{
		NewFunc:                  func() runtime.Object { return &clusteroperator.ClusterDeployment{} },
		NewListFunc:              func() runtime.Object { return &clusteroperator.ClusterDeploymentList{} },
		DefaultQualifiedResource: clusteroperator.Resource("clusterdeployments"),

		CreateStrategy:          clusterDeploymentRESTStrategies,
		UpdateStrategy:          clusterDeploymentRESTStrategies,
		DeleteStrategy:          clusterDeploymentRESTStrategies,
		EnableGarbageCollection: true,
	}

	options := &generic.StoreOptions{RESTOptions: opts}
	if err := store.CompleteWithOptions(options); err != nil {
		panic(err) // TODO: Propagate error up
	}

	statusStore := store
	statusStore.UpdateStrategy = clusterDeploymentStatusUpdateStrategy

	return &store, &StatusREST{&statusStore}
}

// StatusREST defines the REST operations for the status subresource via
// implementation of various rest interfaces.  It supports the http verbs GET,
// PATCH, and PUT.
type StatusREST struct {
	store *registry.Store
}

// New returns a new ClusterDeployment.
func (r *StatusREST) New() runtime.Object {
	return &clusteroperator.ClusterDeployment{}
}

// Get retrieves the object from the storage. It is required to support Patch
// and to implement the rest.Getter interface.
func (r *StatusREST) Get(ctx genericapirequest.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

// Update alters the status subset of an object and implements the
// rest.Updater interface.
func (r *StatusREST) Update(ctx genericapirequest.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc) (runtime.Object, bool, error) {
	return r.store.Update(ctx, name, objInfo, createValidation, updateValidation)
}
