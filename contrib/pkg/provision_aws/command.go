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

package provision_aws

import (
	"fmt"
	"os"
	"os/user"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/discovery"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"

	clustopclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
)

type ProvisionClusterOptions struct {
	Namespace           string
	Name                string
	ImageFormat         string
	RPMRepositoryURL    string
	RPMRepositorySource string
	SSHSecretName       string
	AccountSecretName   string
	KeyPairName         string
	AnsibleImage        string
	Region              string
	KubeconfigFile      string
	Version             string
	VPCName             string
	AMIVPCName          string
	LogLevel            string
	EtcdImage           string

	ClusterName string
	ImageID     string
	Logger      log.FieldLogger
}

func NewProvisionClusterCommand() *cobra.Command {
	userName := "user"
	u, err := user.Current()
	if err == nil {
		userName = u.Username
	}

	opt := &ProvisionClusterOptions{}
	cmd := &cobra.Command{
		Use:   "provision-aws NAME",
		Short: "Provision a new cluster with the given RPM repository and Image Format on AWS",
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
	flags.StringVarP(&opt.Namespace, "namespace", "", "", "Namespace to use. Defaults to current namespace")
	flags.StringVar(&opt.AccountSecretName, "aws-creds-secret", fmt.Sprintf("%s-aws-creds", userName), "Secret to use for AWS account")
	flags.StringVar(&opt.SSHSecretName, "ssh-secret", "libra-ssh", "Secret with SSH key")
	flags.StringVar(&opt.AnsibleImage, "ansible-image", "registry.svc.ci.openshift.org/openshift-cluster-operator/cluster-operator-ansible:latest", "Ansible image to use for job")
	flags.StringVar(&opt.RPMRepositoryURL, "rpm-repository", "", "RPM repository URL for repository that contains OpenShift RPMs. This repository is used to build a new AMI")
	flags.StringVar(&opt.RPMRepositorySource, "rpm-repository-source", "https://gcsweb-ci.svc.ci.openshift.org/gcs/origin-ci-test/releases/openshift/origin/master/origin.repo", "URL of a YUM repository file that points to an RPM repository for Openshift RPMs. This can be used instead of specifying an RPM repository URL directly")
	flags.StringVar(&opt.VPCName, "vpc", "", "VPC to use for the cluster; if blank, a new VPC will be created")
	flags.StringVar(&opt.AMIVPCName, "ami-vpc", "default", "VPC to use to build the AMI")
	flags.StringVar(&opt.LogLevel, "loglevel", "info", "Log level, one of: debug, info, warn, error, fatal, panic")
	// TODO: It is unclear whether this is needed if we're already providing an RPM repo and ImageFormat
	flags.StringVar(&opt.Version, "openshift-version", "3.11", "Version of Openshift to provision")
	flags.StringVar(&opt.ImageFormat, "images", "registry.svc.ci.openshift.org/openshift/origin-v3.11:${component}", "Image format for images to use in the installation")
	flags.StringVar(&opt.KeyPairName, "keypair", "libra", "Name of AWS keypair to use for created instances")
	flags.StringVar(&opt.Region, "region", "us-east-1", "AWS region to use to provision cluster")
	flags.StringVar(&opt.KubeconfigFile, "kubeconfig", "", "Name of a kubeconfig file to create once the control plane is provisioned")
	flags.StringVar(&opt.ImageID, "ami", "", "[Optional] Image ID to use to provision the cluster; if blank, a new AMI is created")
	flags.StringVar(&opt.EtcdImage, "etcd-image", "quay.io/coreos/etcd:v3.2.22", "Image to use for Etcd on target cluster")
	return cmd
}

func (o *ProvisionClusterOptions) Complete(args []string) error {
	if len(args) > 0 {
		o.Name = args[0]
	} else {
		o.Name = names.SimpleNameGenerator.GenerateName("cluster-")
	}

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

func (o *ProvisionClusterOptions) Validate() error {
	if len(o.RPMRepositoryURL) == 0 && len(o.RPMRepositorySource) == 0 {
		return fmt.Errorf("you must specify an RPM repository URL or an RPM repository URL source")
	}
	if len(o.AccountSecretName) == 0 {
		return fmt.Errorf("you must specify an AWS account secret name")
	}
	if len(o.SSHSecretName) == 0 {
		return fmt.Errorf("you must specify an SSH account secret name")
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

func (o *ProvisionClusterOptions) Run() error {
	clients, err := o.getClients()
	if err != nil {
		return err
	}
	if len(o.Namespace) == 0 {
		o.Namespace = clients.Namespace
	}
	o.Logger = o.Logger.WithField("namespace", o.Namespace)
	err = o.PreflightChecks(clients)
	if err != nil {
		return err
	}
	var amiName string
	if len(o.ImageID) == 0 {
		amiName = names.SimpleNameGenerator.GenerateName(fmt.Sprintf("%s-buildami-", o.Name))
		err = o.SaveAMIName(amiName, clients)
		if err != nil {
			return err
		}
		err = o.BuildAMI(amiName, clients)
		if err != nil {
			return err
		}
	}
	servingCert, err := o.GenerateServingCert()
	if err != nil {
		return err
	}
	artifacts := o.GenerateClusterArtifacts(amiName, servingCert)
	err = o.CreateArtifacts(clients, artifacts)
	if err != nil {
		return err
	}
	err = o.WaitForCluster()
	if err != nil {
		return err
	}
	err = o.SaveKubeconfig(clients)
	if err != nil {
		return err
	}
	return nil
}

func (o *ProvisionClusterOptions) getClients() (*clientContext, error) {
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
