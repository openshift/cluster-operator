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

// Waits for a given apiservice to exist and become available

import (
	"fmt"
	"os"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
)

var log *logger.Entry

func main() {
	cmd := NewWaitForAPIServiceCommand()
	cmd.Execute()
}

type WaitForAPIServiceOptions struct {
	Name    string
	Timeout time.Duration
}

func NewWaitForAPIServiceCommand() *cobra.Command {
	opt := &WaitForAPIServiceOptions{}
	logLevel := "info"
	cmd := &cobra.Command{
		Use:   "wait-for-apiservice SERVICENAME",
		Short: "Wait for an API service to exist and become available",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				cmd.Usage()
				return
			}

			// Set log level
			level, err := logger.ParseLevel(logLevel)
			if err != nil {
				logger.WithError(err).Error("Cannot parse log level")
				os.Exit(1)
			}

			log = logger.NewEntry(&logger.Logger{
				Out: os.Stdout,
				Formatter: &logger.TextFormatter{
					FullTimestamp: true,
				},
				Hooks: make(logger.LevelHooks),
				Level: level,
			})

			// Run command
			opt.Name = args[0]
			err = opt.WaitForAPIService()
			if err != nil {
				log.Errorf("Error: %v", err)
				os.Exit(1)
			}
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&logLevel, "loglevel", "info", "log level, one of: debug, info, warn, error, fatal, panic")
	flags.DurationVar(&opt.Timeout, "timeout", 5*time.Minute, "time to wait for API service resource to be ready")
	return cmd
}

func (o *WaitForAPIServiceOptions) WaitForAPIService() error {
	client, err := o.getClient()
	if err != nil {
		log.WithError(err).Error("Cannot obtain cluster client")
		return err
	}
	err = o.waitForAPIService(client)
	if err != nil {
		return err
	}
	log.Infof("APIService %s is ready", o.Name)
	return nil
}

func (o *WaitForAPIServiceOptions) waitForAPIService(client discovery.DiscoveryInterface) error {
	log.Debug("Waiting for API service")
	parts := strings.Split(o.Name, ".")
	groupVersion := fmt.Sprintf("%s/%s", strings.Join(parts[1:], "."), parts[0])
	err := wait.PollImmediate(5*time.Second, o.Timeout, func() (bool, error) {
		log.WithField("groupversion", groupVersion).Debug("Fetching resources")
		_, err := client.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			log.WithError(err).WithField("groupversion", groupVersion).Debug("Error fetching resources")
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			log.WithField("timeout", o.Timeout).Error("Timed out waiting for APIService in discovery output")
		} else {
			log.WithError(err).Error("Unexpected error waiting to find APIService in discovery output")
		}
		return err
	}
	log.Info("APIService found in discovery output")
	return nil
}

func (o *WaitForAPIServiceOptions) getClient() (discovery.DiscoveryInterface, error) {
	log.Debug("Obtaining discovery client for local cluster")
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	cfg, err := kubeconfig.ClientConfig()
	if err != nil {
		log.WithError(err).Error("Cannot obtain client config")
		return nil, err
	}
	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create discovery client from client config")
		return nil, err
	}
	log.Debug("Obtained discovery client for local cluster")
	return client, nil
}
