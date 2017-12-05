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
	"github.com/openshift/cluster-operator/cmd/cluster-operator-controller-manager/app"
	"github.com/openshift/cluster-operator/cmd/cluster-operator-controller-manager/app/options"
	"github.com/openshift/cluster-operator/pkg/hyperkube"
)

// NewControllerManager creates a new hyperkube Server object that includes the
// description and flags.
func NewControllerManager() *hyperkube.Server {
	s := options.NewCMServer()

	hks := hyperkube.Server{
		PrimaryName:     "controller-manager",
		AlternativeName: "cluster-operator-controller-manager",
		SimpleUsage:     "controller-manager",
		Long:            `The clusteroperator controller manager is a daemon that embeds the core control loops shipped with the clusteroperator.`,
		Run: func(_ *hyperkube.Server, args []string, stopCh <-chan struct{}) error {
			return app.Run(s)
		},
		RespectsStopCh: false,
	}
	s.AddFlags(hks.Flags(), app.KnownControllers(), app.ControllersDisabledByDefault.List())
	return &hks

	return nil
}
