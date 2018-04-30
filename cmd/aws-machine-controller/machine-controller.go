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

package main

import (
	"os"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	log "github.com/sirupsen/logrus"

	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/client-go/kubernetes"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/controller"

	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/pkg/controller/config"
	"sigs.k8s.io/cluster-api/pkg/controller/machine"
	"sigs.k8s.io/cluster-api/pkg/controller/sharedinformers"

	"github.com/openshift/cluster-operator/pkg/clusterapi/aws"
)

var (
	defaultAvailabilityZone string
	logLevel                string
)

const (
	controllerLogName = "awsMachine"
	defaultLogLevel   = "info"
)

func init() {
	config.ControllerConfig.AddFlags(pflag.CommandLine)
	pflag.CommandLine.StringVar(&defaultAvailabilityZone, "default-availability-zone", "us-east-1c", "Default availability zone for machines created by this controller.")
	pflag.CommandLine.StringVar(&logLevel, "log-level", defaultLogLevel, "Log level (debug,info,warn,error,fatal)")
}

func main() {
	pflag.Parse()

	logs.InitLogs()
	defer logs.FlushLogs()

	config, err := controller.GetConfig(config.ControllerConfig.Kubeconfig)
	if err != nil {
		glog.Fatalf("Could not create Config for talking to the apiserver: %v", err)
	}

	client, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create kubernetes client to talk to the apiserver: %v", err)
	}

	log.SetOutput(os.Stdout)
	if lvl, err := log.ParseLevel(logLevel); err != nil {
		log.Panic(err)
	} else {
		log.SetLevel(lvl)
	}

	logger := log.WithField("controller", controllerLogName)
	actuator := aws.NewActuator(kubeClient, client, logger, defaultAvailabilityZone)
	shutdown := make(chan struct{})
	si := sharedinformers.NewSharedInformers(config, shutdown)
	c := machine.NewMachineController(config, si, actuator)
	c.Run(shutdown)
	select {}
}
