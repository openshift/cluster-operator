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
	"github.com/openshift/cluster-operator/pkg/api"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	clusteroperatorv1alpha1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/cluster"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/machine"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/machineset"
	"github.com/openshift/cluster-operator/pkg/storage/etcd"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage"
	restclient "k8s.io/client-go/rest"
)

// StorageProvider provides a factory method to create a new APIGroupInfo for
// the clusteroperator API group. It implements (./pkg/apiserver).RESTStorageProvider
type StorageProvider struct {
	DefaultNamespace string
	RESTClient       restclient.Interface
}

// NewRESTStorage is a factory method to make a new APIGroupInfo for the
// clusteroperator API group.
func (p StorageProvider) NewRESTStorage(
	apiResourceConfigSource serverstorage.APIResourceConfigSource,
	restOptionsGetter generic.RESTOptionsGetter,
) (*genericapiserver.APIGroupInfo, error) {

	storage, err := p.v1alpha1Storage(apiResourceConfigSource, restOptionsGetter)
	if err != nil {
		return nil, err
	}

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(clusteroperator.GroupName, api.Registry, api.Scheme, api.ParameterCodec, api.Codecs)
	apiGroupInfo.GroupMeta.GroupVersion = clusteroperatorv1alpha1.SchemeGroupVersion

	apiGroupInfo.VersionedResourcesStorageMap = map[string]map[string]rest.Storage{
		clusteroperatorv1alpha1.SchemeGroupVersion.Version: storage,
	}

	return &apiGroupInfo, nil
}

func (p StorageProvider) v1alpha1Storage(
	apiResourceConfigSource serverstorage.APIResourceConfigSource,
	restOptionsGetter generic.RESTOptionsGetter,
) (map[string]rest.Storage, error) {
	clusterRESTOptions, err := restOptionsGetter.GetRESTOptions(clusteroperator.Resource("clusters"))
	if err != nil {
		return nil, err
	}
	clusterOpts := etcd.Options{
		RESTOptions:   clusterRESTOptions,
		Capacity:      1000,
		ObjectType:    cluster.EmptyObject(),
		ScopeStrategy: cluster.NewScopeStrategy(),
		NewListFunc:   cluster.NewList,
		GetAttrsFunc:  cluster.GetAttrs,
		Trigger:       storage.NoTriggerPublisher,
	}

	clusterStorage, clusterStatusStorage := cluster.NewStorage(clusterOpts)

	machineGroupRESTOptions, err := restOptionsGetter.GetRESTOptions(clusteroperator.Resource("machinesets"))
	if err != nil {
		return nil, err
	}
	machineGroupOpts := etcd.Options{
		RESTOptions:   machineGroupRESTOptions,
		Capacity:      1000,
		ObjectType:    machineset.EmptyObject(),
		ScopeStrategy: machineset.NewScopeStrategy(),
		NewListFunc:   machineset.NewList,
		GetAttrsFunc:  machineset.GetAttrs,
		Trigger:       storage.NoTriggerPublisher,
	}

	machineGroupStorage, machineGroupStatusStorage := machineset.NewStorage(machineGroupOpts)

	machineRESTOptions, err := restOptionsGetter.GetRESTOptions(clusteroperator.Resource("machines"))
	if err != nil {
		return nil, err
	}
	machineOpts := etcd.Options{
		RESTOptions:   machineRESTOptions,
		Capacity:      1000,
		ObjectType:    machine.EmptyObject(),
		ScopeStrategy: machine.NewScopeStrategy(),
		NewListFunc:   machine.NewList,
		GetAttrsFunc:  machine.GetAttrs,
		Trigger:       storage.NoTriggerPublisher,
	}

	machineStorage, machineStatusStorage := machine.NewStorage(machineOpts)

	return map[string]rest.Storage{
		"clusters":           clusterStorage,
		"clusters/status":    clusterStatusStorage,
		"machinesets":        machineGroupStorage,
		"machinesets/status": machineGroupStatusStorage,
		"machines":           machineStorage,
		"machines/status":    machineStatusStorage,
	}, nil
}

// GroupName returns the API group name.
func (p StorageProvider) GroupName() string {
	return clusteroperator.GroupName
}
