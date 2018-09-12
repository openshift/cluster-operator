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
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift/cluster-operator/contrib/pkg/actuator"
	"github.com/openshift/cluster-operator/contrib/pkg/amibuild"
	"github.com/openshift/cluster-operator/contrib/pkg/amiid"
	"github.com/openshift/cluster-operator/contrib/pkg/apiservice"
	"github.com/openshift/cluster-operator/contrib/pkg/cluster"
	"github.com/openshift/cluster-operator/contrib/pkg/deprovision_aws"
	"github.com/openshift/cluster-operator/contrib/pkg/jenkins"
	"github.com/openshift/cluster-operator/contrib/pkg/playbookmock"
	"github.com/openshift/cluster-operator/contrib/pkg/provision_aws"
	"github.com/openshift/cluster-operator/contrib/pkg/verification"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	cmd := NewCOUtilityCommand()

	err := cmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}

func NewCOUtilityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coutil SUB-COMMAND",
		Short: "Utilities for cluster operator",
		Long:  "Contains various utilities for running and testing cluster operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
		},
	}
	cmd.AddCommand(actuator.NewAWSActuatorTestCommand())
	cmd.AddCommand(apiservice.NewWaitForAPIServiceCommand())
	cmd.AddCommand(cluster.NewWaitForClusterCommand())
	cmd.AddCommand(jenkins.NewExtractLogsCommand())
	cmd.AddCommand(playbookmock.NewPlaybookMockCommand())
	cmd.AddCommand(amibuild.NewBuildAMICommand())
	cmd.AddCommand(amiid.NewGetAMIIDCommand())
	cmd.AddCommand(provision_aws.NewProvisionClusterCommand())
	cmd.AddCommand(deprovision_aws.NewDeprovisionClusterCommand())
	cmd.AddCommand(verification.NewVerifyImportsCommand())

	return cmd
}
