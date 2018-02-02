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

package registrytest

import (
	"mime"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/recognizer"
	etcdtesting "k8s.io/apiserver/pkg/storage/etcd/testing"
	"k8s.io/apiserver/pkg/storage/storagebackend"

	"github.com/openshift/cluster-operator/pkg/api"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// NewEtcdStorage creates a new etcd storage for the clusteroperator schema.
func NewEtcdStorage(t *testing.T) (*storagebackend.Config, *etcdtesting.EtcdTestServer) {
	server, config := etcdtesting.NewUnsecuredEtcd3TestClientServer(t)
	mediaType, _, err := mime.ParseMediaType(runtime.ContentTypeJSON)
	if err != nil {
		t.Errorf("failed to parse media type: %v", err)
	}
	storageSerializer, ok := runtime.SerializerInfoForMediaType(api.Codecs.SupportedMediaTypes(), mediaType)
	if !ok {
		t.Errorf("no serializer for %s", mediaType)
	}
	s := storageSerializer.Serializer
	ds := recognizer.NewDecoder(s, api.Codecs.UniversalDeserializer())
	config.Codec = api.Codecs.CodecForVersions(s, ds, schema.GroupVersions{clusteroperator.SchemeGroupVersion}, nil)
	return config, server
}
