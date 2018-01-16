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
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/clusterversion"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/machine"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/machineset"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
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
	clusterStorage, clusterStatusStorage := cluster.NewStorage(clusterRESTOptions)

	clusterVerRESTOptions, err := restOptionsGetter.GetRESTOptions(clusteroperator.Resource("clusterversions"))
	if err != nil {
		return nil, err
	}
	clusterVersionStorage, clusterVersionStatusStorage := clusterversion.NewStorage(clusterVerRESTOptions)

	machineSetRESTOptions, err := restOptionsGetter.GetRESTOptions(clusteroperator.Resource("machinesets"))
	if err != nil {
		return nil, err
	}
	machineSetStorage, machineSetStatusStorage := machineset.NewStorage(machineSetRESTOptions)

	machineRESTOptions, err := restOptionsGetter.GetRESTOptions(clusteroperator.Resource("machines"))
	if err != nil {
		return nil, err
	}
	machineStorage, machineStatusStorage := machine.NewStorage(machineRESTOptions)

	return map[string]rest.Storage{
		"clusters":               clusterStorage,
		"clusters/status":        clusterStatusStorage,
		"clusterversions":        clusterVersionStorage,
		"clusterversions/status": clusterVersionStatusStorage,
		"machinesets":            machineSetStorage,
		"machinesets/status":     machineSetStatusStorage,
		"machines":               machineStorage,
		"machines/status":        machineStatusStorage,
	}, nil
}

// GroupName returns the API group name.
func (p StorageProvider) GroupName() string {
	return clusteroperator.GroupName
}
