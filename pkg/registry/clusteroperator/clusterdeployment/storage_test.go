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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	etcdtesting "k8s.io/apiserver/pkg/storage/etcd/testing"

	clusteroperatorapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	"github.com/openshift/cluster-operator/pkg/registry/registrytest"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

func newStorage(t *testing.T) (*genericregistry.Store, *etcdtesting.EtcdTestServer) {
	etcdStorage, server := registrytest.NewEtcdStorage(t)
	restOptions := generic.RESTOptions{
		StorageConfig:           etcdStorage,
		Decorator:               generic.UndecoratedStorage,
		DeleteCollectionWorkers: 1,
		ResourcePrefix:          "clusterdeployments",
	}
	clusterStorage, _ := NewStorage(restOptions)
	return clusterStorage, server
}

func validNewClusterDeployment(name string) *clusteroperatorapi.ClusterDeployment {
	return &clusteroperatorapi.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusteroperatorapi.ClusterDeploymentSpec{
			MachineSets: []clusteroperatorapi.ClusterMachineSet{
				{
					MachineSetConfig: clusteroperatorapi.MachineSetConfig{
						NodeType: clusteroperatorapi.NodeTypeMaster,
						Infra:    true,
						Size:     1,
					},
				},
			},
			ClusterVersionRef: clusteroperatorapi.ClusterVersionReference{
				Namespace: "openshift-cluster-operator",
				Name:      "v3-9",
			},
			Config: clusteroperatorapi.ClusterConfigSpec{
				SDNPluginName: "redhat/openshift-ovs-multitenant",
			},
			NetworkConfig: capiv1.ClusterNetworkingConfig{
				Services:      capiv1.NetworkRanges{CIDRBlocks: []string{"172.30.0.0/16"}},
				Pods:          capiv1.NetworkRanges{CIDRBlocks: []string{"10.128.0.0/14"}},
				ServiceDomain: "svc.cluster.local",
			},
			Hardware: clusteroperatorapi.ClusterHardwareSpec{
				AWS: &clusteroperatorapi.AWSClusterSpec{},
			},
		},
	}
}

func validChangedClusterDeployment() *clusteroperatorapi.ClusterDeployment {
	return validNewClusterDeployment("foo")
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	clusterDeployment := validNewClusterDeployment("foo")
	clusterDeployment.ObjectMeta = metav1.ObjectMeta{GenerateName: "foo"}
	test.TestCreate(
		// valid
		clusterDeployment,
		// invalid
		&clusteroperatorapi.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: "*BadName!"},
			Spec: clusteroperatorapi.ClusterDeploymentSpec{
				Hardware: clusteroperatorapi.ClusterHardwareSpec{
					AWS: &clusteroperatorapi.AWSClusterSpec{},
				},
			},
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
		func() runtime.Object {
			cd := validNewClusterDeployment("foo")
			cd.Spec.ClusterName = "cluster-id"
			return cd
		}(),
		// updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*clusteroperatorapi.ClusterDeployment)
			return object
		},
		//invalid update
		func(obj runtime.Object) runtime.Object {
			object := obj.(*clusteroperatorapi.ClusterDeployment)
			return object
		},
	)
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage).ReturnDeletedObject()
	test.TestDelete(validNewClusterDeployment("foo"))
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewClusterDeployment("foo"))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	defer storage.DestroyFunc()
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewClusterDeployment("foo"))
}

// TODO: Figure out how to make this test pass.
//func TestWatch(t *testing.T) {
//	storage, server := newStorage(t)
//	defer server.Terminate(t)
//	defer storage.DestroyFunc()
//	test := genericregistrytest.New(t, storage)
//	test.TestWatch(
//		validNewClusterDeployment("foo"),
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
