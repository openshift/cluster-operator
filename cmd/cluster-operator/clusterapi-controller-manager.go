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

package main

import (
	"fmt"
	"path"

	"github.com/openshift/cluster-operator/pkg/hyperkube"

	controllerlib "github.com/kubernetes-incubator/apiserver-builder/pkg/controller"
	"sigs.k8s.io/cluster-api/pkg/controller"
	"sigs.k8s.io/cluster-api/pkg/controller/config"
)

const (
	// program name for debug builds
	debugProgramName = "debug"
)

// NewClusterAPIControllerManager creates a new hyperkube Server object that includes the
// description and flags.
func NewClusterAPIControllerManager(programName string) *hyperkube.Server {
	altName := "cluster-api-controller-manager"

	if path.Base(programName) == debugProgramName {
		altName = debugProgramName
	}

	hks := hyperkube.Server{
		PrimaryName:     "cluster-api-controller-manager",
		AlternativeName: altName,
		SimpleUsage:     "cluster-api-controller-manager",
		Long:            `The cluster API controller manager is a daemon that embeds the core control loops shipped with cluster API.`,
		Run: func(_ *hyperkube.Server, args []string, stopCh <-chan struct{}) error {
			config, err := controllerlib.GetConfig(config.ControllerConfig.Kubeconfig)
			if err != nil {
				return fmt.Errorf("Could not create Config for talking to the apiserver: %v", err)
			}
			controllers, _ := controller.GetAllControllers(config)
			controllerlib.StartControllerManager(controllers...)
			select {
			case <-stopCh:
				return nil
			}
			return nil
		},
		RespectsStopCh: false,
	}
	config.ControllerConfig.AddFlags(hks.Flags())
	return &hks
}
