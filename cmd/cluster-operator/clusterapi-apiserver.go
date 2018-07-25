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

package main

import (
	"os"

	_ "github.com/go-openapi/loads"
	_ "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/cmd/server"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Enable cloud provider auth

	"sigs.k8s.io/cluster-api/pkg/apis"

	"github.com/openshift/cluster-operator/pkg/hyperkube"
)

// NewClusterAPIServer creates a new hyperkube Server object that includes the
// description and flags.
func NewClusterAPIServer() *hyperkube.Server {

	// APIServer parameters
	etcdPath := "/registry/k8s.io"
	apis := apis.GetAllApiBuilders()
	serverStopCh := make(chan struct{})
	title := "Api"
	version := "v0"

	// APIServer command
	cmd, _ := server.NewCommandStartServer(etcdPath, os.Stdout, os.Stderr, apis, serverStopCh, title, version)

	hks := hyperkube.Server{
		PrimaryName: "cluster-api-server",
		SimpleUsage: "cluster-api-server",
		Long:        cmd.Long,
		Run: func(_ *hyperkube.Server, args []string, stopCh <-chan struct{}) error {
			go func() {
				select {
				case <-stopCh:
					serverStopCh <- struct{}{}
				}
			}()
			return cmd.Execute()
		},
		RespectsStopCh: true,
	}

	// Set HyperKube command flags
	hks.Flags().AddFlagSet(cmd.Flags())

	return &hks
}
