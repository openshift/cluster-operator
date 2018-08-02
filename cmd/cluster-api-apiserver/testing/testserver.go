/*
Copyright 2018 The Kubernetes Authors.

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

package testing

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apimachinery/announced"
	"k8s.io/apimachinery/pkg/apimachinery/registered"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	genericapiserver "k8s.io/apiserver/pkg/server"
	etcdtesting "k8s.io/apiserver/pkg/storage/etcd/testing"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/apiserver"
	"github.com/kubernetes-incubator/apiserver-builder/pkg/builders"
	"github.com/kubernetes-incubator/apiserver-builder/pkg/cmd/server"
	capis "sigs.k8s.io/cluster-api/pkg/apis"
	"sigs.k8s.io/cluster-api/pkg/openapi"
)

// TearDownFunc is to be called to tear down a test server.
type TearDownFunc func()

// StartTestServer starts a etcd server and kube-apiserver. A rest client config and a tear-down func
// are returned.
//
// Note: we return a tear-down func instead of a stop channel because the later will leak temporariy
// 		 files that because Golang testing's call to os.Exit will not give a stop channel go routine
// 		 enough time to remove temporariy files.
func StartTestServer(t *testing.T) (result *restclient.Config, tearDownForCaller TearDownFunc, err error) {
	var tmpDir string
	var etcdServer *etcdtesting.EtcdTestServer
	stopCh := make(chan struct{})
	tearDown := func() {
		close(stopCh)
		if etcdServer != nil {
			etcdServer.Terminate(t)
		}
		if len(tmpDir) != 0 {
			os.RemoveAll(tmpDir)
		}
		builders.GroupFactoryRegistry = announced.APIGroupFactoryRegistry{}
		builders.Registry = registered.NewOrDie(os.Getenv("KUBE_API_VERSIONS"))
		builders.Scheme = runtime.NewScheme()
		builders.Codecs = serializer.NewCodecFactory(builders.Scheme)
		builders.APIGroupBuilders = []*builders.APIGroupBuilder{}
	}
	defer func() {
		if tearDownForCaller == nil {
			tearDown()
		}
	}()

	t.Logf("Starting etcd...")
	etcdServer, storageConfig := etcdtesting.NewUnsecuredEtcd3TestClientServer(t)

	tmpDir, err = ioutil.TempDir("", "openshift-cluster-api-apiserver")
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create temp dir: %v", err)
	}

	server.GetOpenApiDefinition = openapi.GetOpenAPIDefinitions
	s := server.NewServerOptions("/registry/k8s.io", os.Stdout, os.Stderr, capis.GetAllApiBuilders())

	storageConfig.Codec = builders.Codecs.LegacyCodec(capis.GetClusterAPIBuilder().GetLegacyCodec()...)
	s.RunDelegatedAuth = false
	s.RecommendedOptions.SecureServing.BindPort = freePort()
	s.RecommendedOptions.SecureServing.ServerCert.CertDirectory = tmpDir
	s.RecommendedOptions.Etcd.StorageConfig = *storageConfig
	s.RecommendedOptions.Etcd.DefaultStorageMediaType = "application/json"

	t.Logf("Starting cluster-api-apiserver...")
	runErrCh := make(chan error, 1)
	server, err := createServer(s, "cluster-api-apiserver", "v0")
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create server: %v", err)
	}
	go func(stopCh <-chan struct{}) {
		if err := server.GenericAPIServer.PrepareRun().Run(stopCh); err != nil {
			t.Logf("cluster-api-apiserver exited uncleanly: %v", err)
			runErrCh <- err
		}
	}(stopCh)

	t.Logf("Waiting for /healthz to be ok...")
	client, err := kubernetes.NewForConfig(server.GenericAPIServer.LoopbackClientConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create a client: %v", err)
	}
	err = wait.Poll(100*time.Millisecond, 30*time.Second, func() (bool, error) {
		select {
		case err := <-runErrCh:
			return false, err
		default:
		}

		result := client.CoreV1().RESTClient().Get().AbsPath("/healthz").Do()
		status := 0
		result.StatusCode(&status)
		if status == 200 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to wait for /healthz to return ok: %v", err)
	}

	// from here the caller must call tearDown
	return server.GenericAPIServer.LoopbackClientConfig, tearDown, nil
}

// StartTestServerOrDie calls StartTestServer with up to 5 retries on bind error and dies with
// t.Fatal if it does not succeed.
func StartTestServerOrDie(t *testing.T) (*restclient.Config, TearDownFunc) {
	config, td, err := StartTestServer(t)
	if err != nil {
		t.Fatalf("Failed to launch server: %v", err)
	}
	return config, td
}

func freePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func createServer(o *server.ServerOptions, title, version string) (*apiserver.Server, error) {
	config, err := o.Config()
	if err != nil {
		return nil, err
	}
	o.BearerToken = config.GenericConfig.LoopbackClientConfig.BearerToken

	for _, provider := range o.APIBuilders {
		config.AddApi(provider)
	}

	config.Init()

	config.GenericConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(server.GetOpenApiDefinition, builders.Scheme)
	config.GenericConfig.OpenAPIConfig.Info.Title = title
	config.GenericConfig.OpenAPIConfig.Info.Version = version

	server, err := config.Complete().New()
	if err != nil {
		return nil, err
	}

	for _, h := range o.PostStartHooks {
		server.GenericAPIServer.AddPostStartHook(h.Name, h.Fn)
	}
	return server, nil
}
