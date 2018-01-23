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

package machineset

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
	etcdStorage, server := registrytest.NewEtcdStorage(t, clusteroperatorapi.GroupName)
	restOptions := generic.RESTOptions{
		StorageConfig:           etcdStorage,
		Decorator:               generic.UndecoratedStorage,
		DeleteCollectionWorkers: 1,
		ResourcePrefix:          "machinesets",
	}
	machineSetStorage, _ := NewStorage(restOptions)
	return machineSetStorage, server
}

func validNewMachineSet(name string) *clusteroperatorapi.MachineSet {
	return &clusteroperatorapi.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "clusteroperator.openshift.io/v1alpha1",
					Kind:       "Cluster",
					Name:       "cluster-name",
					UID:        "cluster-uid",
					Controller: func(b bool) *bool { return &b }(true),
				},
			},
		},
		Spec: clusteroperatorapi.MachineSetSpec{
			MachineSetConfig: clusteroperatorapi.MachineSetConfig{
				NodeType: clusteroperatorapi.NodeTypeMaster,
				Size:     1,
			},
			Version: clusteroperatorapi.ClusterVersionReference{
				Namespace: "cluster-operator",
				Name:      "v3-9",
			},
		},
	}
}

func validChangedMachineSet() *clusteroperatorapi.MachineSet {
	return validNewMachineSet("foo")
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	machineSet := validNewMachineSet("")
	machineSet.GenerateName = "foo"
	test.TestCreate(
		// valid
		machineSet,
		// invalid
		&clusteroperatorapi.MachineSet{
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
		validNewMachineSet("foo"),
		// updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*clusteroperatorapi.MachineSet)
			return object
		},
		//invalid update
		func(obj runtime.Object) runtime.Object {
			object := obj.(*clusteroperatorapi.MachineSet)
			return object
		},
	)
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage).ReturnDeletedObject()
	test.TestDelete(validNewMachineSet("foo"))
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewMachineSet("foo"))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewMachineSet("foo"))
}

// TODO: Figure out how to make this test pass.
//func TestWatch(t *testing.T) {
//	storage, server := newStorage(t)
//	defer server.Terminate(t)
//	defer storage.DestroyFunc()
//	test := genericregistrytest.New(t, storage)
//	test.TestWatch(
//		validNewMachineSet("foo"),
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
