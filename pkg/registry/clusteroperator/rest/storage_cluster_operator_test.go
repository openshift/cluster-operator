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

package rest

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
)

type GetRESTOptionsHelper struct {
	retStorageInterface storage.Interface
	retDestroyFunc      func()
}

func (g GetRESTOptionsHelper) GetRESTOptions(resource schema.GroupResource) (generic.RESTOptions, error) {
	return generic.RESTOptions{
		ResourcePrefix: resource.Group + "/" + resource.Resource,
		StorageConfig:  &storagebackend.Config{},
		Decorator: generic.StorageDecorator(func(
			copier runtime.ObjectCopier,
			config *storagebackend.Config,
			objectType runtime.Object,
			resourcePrefix string,
			keyFunc func(obj runtime.Object) (string, error),
			newListFunc func() runtime.Object,
			getAttrsFunc storage.AttrFunc,
			trigger storage.TriggerPublisherFunc,
		) (storage.Interface, factory.DestroyFunc) {
			return g.retStorageInterface, g.retDestroyFunc
		})}, nil
}

func testRESTOptionsGetter(
	retStorageInterface storage.Interface,
	retDestroyFunc func(),
) generic.RESTOptionsGetter {
	return GetRESTOptionsHelper{retStorageInterface, retDestroyFunc}
}

func TestV1Alpha1Storage(t *testing.T) {
	provider := StorageProvider{
		DefaultNamespace: "test-default",
		RESTClient:       nil,
	}
	configSource := serverstorage.NewResourceConfig()
	roGetter := testRESTOptionsGetter(nil, func() {})
	storageMap, err := provider.v1alpha1Storage(configSource, roGetter)
	if err != nil {
		t.Fatalf("error getting v1alpha1 storage (%s)", err)
	}

	_, clusterStorageExists := storageMap["clusters"]
	if !clusterStorageExists {
		t.Fatalf("no clusters storage found")
	}
	_, clusterStatusStorageExists := storageMap["clusters/status"]
	if !clusterStatusStorageExists {
		t.Fatalf("no clusters/status storage found")
	}

	_, nodeGroupStorageExists := storageMap["nodegroups"]
	if !nodeGroupStorageExists {
		t.Fatalf("no nodegroups storage found")
	}
	_, nodeGroupStatusStorageExists := storageMap["nodegroups/status"]
	if !nodeGroupStatusStorageExists {
		t.Fatalf("no nodegroups/status storage found")
	}

	_, nodeStorageExists := storageMap["nodes"]
	if !nodeStorageExists {
		t.Fatalf("no nodes storage found")
	}
	_, nodeStatusStorageExists := storageMap["nodes/status"]
	if !nodeStatusStorageExists {
		t.Fatalf("no nodes/status storage found")
	}
}
