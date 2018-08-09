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

package amibuild

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"strings"

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type BuildAMIOptions struct {
	AccountSecretName      string
	SSHSecretName          string
	AnsibleImage           string
	AnsibleImagePullPolicy corev1.PullPolicy
	RPMRepositoryURL       string
	RPMRepositorySource    string
	Namespace              string
	LogLevel               string
	VPCName                string
	Logger                 log.FieldLogger
	Name                   string
}

func NewBuildAMICommand() *cobra.Command {
	userName := "user"
	u, err := user.Current()
	if err == nil {
		userName = u.Username
	}

	opt := &BuildAMIOptions{
		AnsibleImagePullPolicy: corev1.PullIfNotPresent,
	}
	cmd := &cobra.Command{
		Use:   "build-ami NAME",
		Short: "Build an AMI with the given RPM repository",
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
	flags.StringVarP(&opt.Namespace, "namespace", "", "", "namespace to use. Defaults to current namespace.")
	flags.StringVar(&opt.AccountSecretName, "account-secret", fmt.Sprintf("%s-aws-creds", userName), "secret to use for AWS account")
	flags.StringVar(&opt.SSHSecretName, "ssh-secret", "libra-ssh", "secret with SSH key")
	flags.StringVar(&opt.AnsibleImage, "ansible-image", "cluster-operator-ansible:canary", "ansible image to use for job")
	flags.StringVar(&opt.RPMRepositoryURL, "rpm-repository", "", "RPM repository for Origin")
	flags.StringVar(&opt.RPMRepositorySource, "rpm-repository-source", "https://gcsweb-ci.svc.ci.openshift.org/gcs/origin-ci-test/releases/openshift/origin/master/origin.repo", "pointer to RPM repository URL")
	flags.StringVar(&opt.VPCName, "vpc", "default", "VPC to use to build the AMI")
	flags.StringVar(&opt.LogLevel, "loglevel", "info", "log level, one of: debug, info, warn, error, fatal, panic")
	return cmd
}

func (o *BuildAMIOptions) Complete(args []string) error {

	if len(args) > 0 {
		o.Name = args[0]
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
	})

	return nil
}

func (o *BuildAMIOptions) Validate() error {
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

func (o *BuildAMIOptions) Run() error {
	var err error
	if len(o.RPMRepositoryURL) == 0 {
		o.RPMRepositoryURL, err = o.determineRPMRepositoryURL(o.RPMRepositorySource)
		if err != nil {
			return fmt.Errorf("cannot retrieve RPM repository URL from %s: %v", o.RPMRepositorySource, err)
		}
		o.Logger.WithField("RepositoryURL", o.RPMRepositoryURL).Debug("Obtained RPM repository URL")
	}
	curNamespace, client, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("cannot get kubernetes client: %v", err)
	}

	if len(o.Namespace) == 0 {
		o.Namespace = curNamespace
	}

	pod, cfgmap, err := o.Generate()
	if err != nil {
		return err
	}
	err = o.RunPod(client, pod, cfgmap)
	if err != nil {
		return err
	}
	log.Infof("AMI generation succeeded")
	return nil
}

func (o *BuildAMIOptions) determineRPMRepositoryURL(sourceURL string) (string, error) {
	resp, err := http.Get(sourceURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	// Simple parsing of ini response
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "baseurl") {
			parts := strings.Split(line, "=")
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("could not determine repository URL")
}

func getKubernetesClient() (string, clientset.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	cfg, err := kubeconfig.ClientConfig()
	if err != nil {
		return "", nil, err
	}
	namespace, _, err := kubeconfig.Namespace()
	if err != nil {
		return "", nil, err
	}
	kubeClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	return namespace, kubeClient, nil
}
