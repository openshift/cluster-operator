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

package deprovision_aws

import (
	"fmt"
	"os"
	"os/user"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"k8s.io/client-go/discovery"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"

	clustopclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
)

type DeprovisionClusterOptions struct {
	Namespace         string
	Name              string
	LogLevel          string
	AccountSecretName string
	Region            string

	Logger  log.FieldLogger
	AMIID   string
	AMIName string
}

func NewDeprovisionClusterCommand() *cobra.Command {
	userName := "user"
	u, err := user.Current()
	if err == nil {
		userName = u.Username
	}

	opt := &DeprovisionClusterOptions{}
	cmd := &cobra.Command{
		Use:   "deprovision-aws NAME",
		Short: "Deprovision an AWS cluster",
		Run: func(cmd *cobra.Command, args []string) {
			if err := opt.Complete(args); err != nil {
				log.WithError(err).Error("Cannot complete command")
				return
			}
			if err := opt.Validate(); err != nil {
				log.WithError(err).Error("Invalid command options")
				return
			}
			if err := opt.Run(); err != nil {
				log.WithError(err).Error("Runtime error")
			}
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opt.AccountSecretName, "account-secret", fmt.Sprintf("%s-aws-creds", userName), "secret to use for AWS account")
	flags.StringVarP(&opt.Namespace, "namespace", "", "", "namespace to use. Defaults to current namespace.")
	flags.StringVar(&opt.LogLevel, "loglevel", "info", "log level, one of: debug, info, warn, error, fatal, panic")
	flags.StringVar(&opt.Region, "region", "us-east-1", "AWS region where the cluster was provisioned")
	return cmd
}

func (o *DeprovisionClusterOptions) Complete(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no cluster name specified")
	}
	o.Name = args[0]

	// Set log level
	level, err := log.ParseLevel(o.LogLevel)
	if err != nil {
		log.WithError(err).Error("Cannot parse log level")
		return err
	}

	o.Logger = log.NewEntry(&log.Logger{
		Out: os.Stdout,
		Formatter: &log.TextFormatter{
			FullTimestamp: true,
		},
		Hooks: make(log.LevelHooks),
		Level: level,
	}).WithField("name", o.Name)
	return nil
}

func (o *DeprovisionClusterOptions) Validate() error {
	if len(o.Name) == 0 {
		return fmt.Errorf("a cluster name must be specified.")
	}
	return nil
}

type clientContext struct {
	Namespace        string
	KubeClient       clientset.Interface
	ClusterAPIClient capiclient.Interface
	ClusterOpClient  clustopclient.Interface
	DiscoveryClient  discovery.DiscoveryInterface
}

func (o *DeprovisionClusterOptions) Run() error {
	o.Logger.Info("Obtaining cluster client")
	clients, err := o.getClients()
	if len(o.Namespace) == 0 {
		o.Namespace = clients.Namespace
	}
	o.Logger = o.Logger.WithField("namespace", o.Namespace)
	o.Logger.Infof("Removing ClusterDeployment")
	err = o.DestroyCluster(clients)
	if err != nil {
		return err
	}
	o.Logger.Infof("Deregistering AMI")
	return o.DestroyAMI(clients)
}

func (o *DeprovisionClusterOptions) getClients() (*clientContext, error) {
	ctx := &clientContext{}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	cfg, err := kubeconfig.ClientConfig()
	if err != nil {
		log.WithError(err).Error("Cannot obtain client configuration")
		return nil, err
	}
	ctx.Namespace, _, err = kubeconfig.Namespace()
	if err != nil {
		log.WithError(err).Error("Cannot obtain default Namespace from current client")
		return nil, err
	}
	ctx.KubeClient, err = clientset.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot obtain Kubernetes client from client config")
		return nil, err
	}
	ctx.DiscoveryClient, err = discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create discovery client from client config")
		return nil, err
	}
	ctx.ClusterAPIClient, err = capiclient.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create cluster API client from client config")
		return nil, err
	}
	ctx.ClusterOpClient, err = clustopclient.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create cluster operator client from client config")
		return nil, err
	}
	return ctx, nil
}
