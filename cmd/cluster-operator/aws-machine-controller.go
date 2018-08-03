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
	"path"

	"github.com/openshift/cluster-operator/cmd/aws-machine-controller/app"
	"github.com/openshift/cluster-operator/pkg/hyperkube"
)

// NewAWSMachineController creates a new hyperkube Server object that invokes
// the AWS machine controller
func NewAWSMachineController(programName string) *hyperkube.Server {
	o := app.NewControllerRunOptions()

	altName := "aws-machine-controller"

	if path.Base(programName) == debugProgramName {
		altName = debugProgramName
	}

	hks := hyperkube.Server{
		PrimaryName:     "aws-machine-controller",
		AlternativeName: altName,
		SimpleUsage:     "aws-machine-controller",
		Long:            "The clusteroperator AWS machine controller manages machine creation and deletion on AWS",
		Run: func(_ *hyperkube.Server, args []string, stopCh <-chan struct{}) error {
			return o.Run(stopCh)
		},
		RespectsStopCh: true,
	}
	o.AddFlags(hks.Flags())
	return &hks
}
