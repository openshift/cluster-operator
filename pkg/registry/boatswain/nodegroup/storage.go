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

package nodegroup

import (
	"errors"
	"fmt"

	"github.com/staebler/boatswain/pkg/api"
	boatswainmeta "github.com/staebler/boatswain/pkg/api/meta"
	"github.com/staebler/boatswain/pkg/apis/boatswain"
	"github.com/staebler/boatswain/pkg/storage/etcd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
)

var (
	errNotANodeGroup = errors.New("not a nodegroup")
)

// NewSingular returns a new shell of a service nodegroup, according to the given namespace and
// name
func NewSingular(ns, name string) runtime.Object {
	return &boatswain.NodeGroup{
		TypeMeta: metav1.TypeMeta{
			Kind: "NodeGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
	}
}

// EmptyObject returns an empty nodegroup
func EmptyObject() runtime.Object {
	return &boatswain.NodeGroup{}
}

// NewList returns a new shell of a nodegroup list
func NewList() runtime.Object {
	return &boatswain.NodeGroupList{
		TypeMeta: metav1.TypeMeta{
			Kind: "NodeGroupList",
		},
		Items: []boatswain.NodeGroup{},
	}
}

// CheckObject returns a non-nil error if obj is not a nodegroup object
func CheckObject(obj runtime.Object) error {
	_, ok := obj.(*boatswain.NodeGroup)
	if !ok {
		return errNotANodeGroup
	}
	return nil
}

// Match determines whether an NodeGroup matches a field and label selector.
func Match(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// toSelectableFields returns a field set that represents the object for matching purposes.
func toSelectableFields(nodegroup *boatswain.NodeGroup) fields.Set {
	objectMetaFieldsSet := generic.ObjectMetaFieldsSet(&nodegroup.ObjectMeta, true)
	return generic.MergeFieldsSets(objectMetaFieldsSet, nil)
}

// GetAttrs returns labels and fields of a given object for filtering purposes.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, bool, error) {
	nodegroup, ok := obj.(*boatswain.NodeGroup)
	if !ok {
		return nil, nil, false, fmt.Errorf("given object is not a NodeGroup")
	}
	return labels.Set(nodegroup.ObjectMeta.Labels), toSelectableFields(nodegroup), nodegroup.Initializers != nil, nil
}

// NewStorage creates a new rest.Storage responsible for accessing
// NodeGroup resources
func NewStorage(opts etcd.Options) (nodegroups, nodegroupsStatus rest.Storage) {
	restOpts := opts.RESTOptions

	prefix := "/" + restOpts.ResourcePrefix

	storageInterface, dFunc := restOpts.Decorator(
		api.Scheme,
		restOpts.StorageConfig,
		&boatswain.NodeGroup{},
		prefix,
		nil, /* keyFunc for decorator -- looks to be unused everywhere */
		NewList,
		nil,
		storage.NoTriggerPublisher,
	)

	store := registry.Store{
		NewFunc:     EmptyObject,
		NewListFunc: NewList,
		KeyRootFunc: func(ctx genericapirequest.Context) string {
			return registry.NamespaceKeyRootFunc(ctx, prefix)
		},
		KeyFunc: func(ctx genericapirequest.Context, name string) (string, error) {
			return registry.NamespaceKeyFunc(ctx, prefix, name)
		},
		// Retrieve the name field of the resource.
		ObjectNameFunc: func(obj runtime.Object) (string, error) {
			return boatswainmeta.GetAccessor().Name(obj)
		},
		// Used to match objects based on labels/fields for list.
		PredicateFunc: Match,
		// DefaultQualifiedResource should always be plural
		DefaultQualifiedResource: boatswain.Resource("clusterservicenodegroups"),

		CreateStrategy:          nodegroupRESTStrategies,
		UpdateStrategy:          nodegroupRESTStrategies,
		DeleteStrategy:          nodegroupRESTStrategies,
		EnableGarbageCollection: true,

		Storage:     storageInterface,
		DestroyFunc: dFunc,
	}

	options := &generic.StoreOptions{RESTOptions: restOpts, AttrFunc: GetAttrs}
	if err := store.CompleteWithOptions(options); err != nil {
		panic(err) // TODO: Propagate error up
	}

	statusStore := store
	statusStore.UpdateStrategy = nodegroupStatusUpdateStrategy

	return &store, &StatusREST{&statusStore}
}

// StatusREST defines the REST operations for the status subresource via
// implementation of various rest interfaces.  It supports the http verbs GET,
// PATCH, and PUT.
type StatusREST struct {
	store *registry.Store
}

// New returns a new NodeGroup.
func (r *StatusREST) New() runtime.Object {
	return &boatswain.NodeGroup{}
}

// Get retrieves the object from the storage. It is required to support Patch
// and to implement the rest.Getter interface.
func (r *StatusREST) Get(ctx genericapirequest.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

// Update alters the status subset of an object and implements the
// rest.Updater interface.
func (r *StatusREST) Update(ctx genericapirequest.Context, name string, objInfo rest.UpdatedObjectInfo) (runtime.Object, bool, error) {
	return r.store.Update(ctx, name, objInfo)
}
