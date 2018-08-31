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
	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/clusterdeployment"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/clusterversion"
	"github.com/openshift/cluster-operator/pkg/registry/clusteroperator/dnszone"
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

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(coapi.GroupName, api.Registry, api.Scheme, api.ParameterCodec, api.Codecs)
	apiGroupInfo.GroupMeta.GroupVersion = cov1.SchemeGroupVersion

	apiGroupInfo.VersionedResourcesStorageMap = map[string]map[string]rest.Storage{
		cov1.SchemeGroupVersion.Version: storage,
	}

	return &apiGroupInfo, nil
}

func (p StorageProvider) v1alpha1Storage(
	apiResourceConfigSource serverstorage.APIResourceConfigSource,
	restOptionsGetter generic.RESTOptionsGetter,
) (map[string]rest.Storage, error) {
	clusterDeploymentRESTOptions, err := restOptionsGetter.GetRESTOptions(coapi.Resource("clusterdeployments"))
	if err != nil {
		return nil, err
	}
	clusterDeploymentStorage, clusterDeploymentStatusStorage := clusterdeployment.NewStorage(clusterDeploymentRESTOptions)

	clusterVerRESTOptions, err := restOptionsGetter.GetRESTOptions(coapi.Resource("clusterversions"))
	if err != nil {
		return nil, err
	}
	clusterVersionStorage, clusterVersionStatusStorage := clusterversion.NewStorage(clusterVerRESTOptions)

	dnsZoneRESTOptions, err := restOptionsGetter.GetRESTOptions(coapi.Resource("dnszones"))
	if err != nil {
		return nil, err
	}
	dnsZoneStorage, dnsZoneStatusStorage := dnszone.NewStorage(dnsZoneRESTOptions)

	return map[string]rest.Storage{
		"clusterdeployments":        clusterDeploymentStorage,
		"clusterdeployments/status": clusterDeploymentStatusStorage,
		"clusterversions":           clusterVersionStorage,
		"clusterversions/status":    clusterVersionStatusStorage,
		"dnszones":                  dnsZoneStorage,
		"dnszones/status":           dnsZoneStatusStorage,
	}, nil
}

// GroupName returns the API group name.
func (p StorageProvider) GroupName() string {
	return coapi.GroupName
}
