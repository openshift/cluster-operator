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

package dnszone

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	etcdtesting "k8s.io/apiserver/pkg/storage/etcd/testing"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	"github.com/openshift/cluster-operator/pkg/registry/registrytest"
)

func newStorage(t *testing.T) (*genericregistry.Store, *etcdtesting.EtcdTestServer) {
	etcdStorage, server := registrytest.NewEtcdStorage(t)
	restOptions := generic.RESTOptions{
		StorageConfig:           etcdStorage,
		Decorator:               generic.UndecoratedStorage,
		DeleteCollectionWorkers: 1,
		ResourcePrefix:          "clustersversions",
	}
	dnsZoneStorage, _ := NewStorage(restOptions)
	return dnsZoneStorage, server
}

func validDNSZone(name string) *coapi.DNSZone {
	return &coapi.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: coapi.DNSZoneSpec{
			Zone: "aws.example.com",
		},
	}
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	hz := validDNSZone("hz1")
	hz.ObjectMeta = metav1.ObjectMeta{GenerateName: "hz1"}
	test.TestCreate(
		// valid
		hz,
		// invalid
		&coapi.DNSZone{
			ObjectMeta: metav1.ObjectMeta{Name: "*BadName!"},
		},
	)
}

func TestUpdate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		// valid
		validDNSZone("hz1"),
		// updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*coapi.DNSZone)
			return object
		},
		//invalid update
		func(obj runtime.Object) runtime.Object {
			object := obj.(*coapi.DNSZone)
			return object
		},
	)
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage).ReturnDeletedObject()
	test.TestDelete(validDNSZone("hz1"))
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestGet(validDNSZone("hz1"))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestList(validDNSZone("hz1"))
}
