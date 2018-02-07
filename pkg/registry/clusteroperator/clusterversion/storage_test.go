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

package clusterversion

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	etcdtesting "k8s.io/apiserver/pkg/storage/etcd/testing"

	clusteroperatorapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
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
	clusterVersionStorage, _ := NewStorage(restOptions)
	return clusterVersionStorage, server
}

func validClusterVersion(name string) *clusteroperatorapi.ClusterVersion {
	return &clusteroperatorapi.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusteroperatorapi.ClusterVersionSpec{
			ImageFormat: "openshift/origin-${component}:v3.7.9",
			VMImages: clusteroperatorapi.VMImages{
				AWSImages: &clusteroperatorapi.AWSVMImages{
					AMIByRegion: map[string]string{
						"us-east-1": "fakeami",
					},
				},
			},
			DeploymentType: clusteroperatorapi.ClusterDeploymentTypeOrigin,
			Version:        "v3.9.0",
		},
	}
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	cv := validClusterVersion("v3.9")
	cv.ObjectMeta = metav1.ObjectMeta{GenerateName: "v3.9"}
	test.TestCreate(
		// valid
		cv,
		// invalid
		&clusteroperatorapi.ClusterVersion{
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
		validClusterVersion("v3.9"),
		// updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*clusteroperatorapi.ClusterVersion)
			return object
		},
		//invalid update
		func(obj runtime.Object) runtime.Object {
			object := obj.(*clusteroperatorapi.ClusterVersion)
			return object
		},
	)
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage).ReturnDeletedObject()
	test.TestDelete(validClusterVersion("v3.9"))
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestGet(validClusterVersion("v3.9"))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestList(validClusterVersion("v3.9"))
}

// TODO: Figure out how to make this test pass.
//func TestWatch(t *testing.T) {
//	storage, server := newStorage(t)
//	defer server.Terminate(t)
//	defer storage.DestroyFunc()
//	test := genericregistrytest.New(t, storage)
//	test.TestWatch(
//		validClusterVersion("v3.9"),
//		// matching labels
//		[]labels.Set{},
//		// not matching labels
//		[]labels.Set{
//			{"foo": "bar"},
//		},
//		// matching fields
//		[]fields.Set{
//			{"metadata.name": "foo"},
//		},
//		// not matching fields
//		[]fields.Set{
//			{"metadata.name": "bar"},
//		},
//	)
//}
