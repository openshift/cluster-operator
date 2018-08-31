/*
Copyright 2014 The Kubernetes Authors.

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

package app

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"

	log "github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/controller"

	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/pkg/controller/config"
	"sigs.k8s.io/cluster-api/pkg/controller/machine"
	"sigs.k8s.io/cluster-api/pkg/controller/sharedinformers"

	coaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws"
)

const (
	controllerLogName       = "awsMachine"
	defaultLogLevel         = "info"
	defaultAvailabilityZone = "us-east-1c"
)

// NewControllerRunOptions creates a new options object to run the AWS machine controller
func NewControllerRunOptions() *AWSMachineControllerOptions {
	return &AWSMachineControllerOptions{
		DefaultAvailabilityZone: defaultAvailabilityZone,
		LogLevel:                defaultLogLevel,
	}
}

// AWSMachineControllerOptions contains options for running the AWS machine controller
type AWSMachineControllerOptions struct {
	DefaultAvailabilityZone string
	LogLevel                string
}

// AddFlags adds the AWS machine controller option flags to an existing FlagSet
func (o *AWSMachineControllerOptions) AddFlags(flagSet *pflag.FlagSet) {
	config.ControllerConfig.AddFlags(flagSet)
	flagSet.StringVar(&o.DefaultAvailabilityZone, "default-availability-zone", o.DefaultAvailabilityZone, "Default availability zone for machines created by this controller.")
	flagSet.StringVar(&o.LogLevel, "log-level", o.LogLevel, "Log level (debug,info,warn,error,fatal)")
}

// Run executes the AWS machine controller until signaled by the stop channel
func (o *AWSMachineControllerOptions) Run(stopCh <-chan struct{}) error {
	config, err := controller.GetConfig(config.ControllerConfig.Kubeconfig)
	if err != nil {
		return fmt.Errorf("could not create Config for talking to the apiserver: %v", err)
	}

	client, err := clientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create client for talking to the apiserver: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create kubernetes client to talk to the apiserver: %v", err)
	}

	log.SetOutput(os.Stdout)
	lvl, err := log.ParseLevel(o.LogLevel)
	if err != nil {
		return fmt.Errorf("could not parse loglevel(%s): %v", o.LogLevel, err)
	}
	log.SetLevel(lvl)

	logger := log.WithField("controller", controllerLogName)
	actuator := coaws.NewActuator(kubeClient, client, logger, o.DefaultAvailabilityZone)
	si := sharedinformers.NewSharedInformers(config, stopCh)
	c := machine.NewMachineController(config, si, actuator)
	c.Run(stopCh)
	select {
	case <-stopCh:
		return nil
	}
}
