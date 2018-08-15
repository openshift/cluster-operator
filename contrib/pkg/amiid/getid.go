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

package amiid

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	awsCredsSecretIDKey     = "awsAccessKeyId"
	awsCredsSecretAccessKey = "awsSecretAccessKey"
)

type Filter struct {
	Name  string
	Value string
}

type GetAMIIDOptions struct {
	Filters           []Filter
	Logger            log.FieldLogger
	AccountSecretName string
	LogLevel          string
	Region            string
	Namespace         string
}

func NewGetAMIIDCommand() *cobra.Command {
	userName := "user"
	u, err := user.Current()
	if err == nil {
		userName = u.Username
	}
	opt := &GetAMIIDOptions{}
	cmd := &cobra.Command{
		Use:   "ami-id FILTER",
		Short: "Retrieve the ID of an AMI given a set of filters",
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
	flags.StringVar(&opt.LogLevel, "loglevel", "info", "log level, one of: debug, info, warn, error, fatal, panic")
	flags.StringVar(&opt.AccountSecretName, "account-secret", fmt.Sprintf("%s-aws-creds", userName), "secret to use for AWS account")
	flags.StringVar(&opt.Region, "region", "us-east-1", "AWS region to use")
	flags.StringVar(&opt.Namespace, "namespace", "", "Kubernetes namespace where account secret lives")
	return cmd
}

func (o *GetAMIIDOptions) Complete(args []string) error {

	for _, arg := range args {
		filter, err := parseFilter(arg)
		if err != nil {
			return fmt.Errorf("cannot parse filter %s: %v", arg, err)
		}
		o.Filters = append(o.Filters, *filter)
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

func (o *GetAMIIDOptions) Validate() error {
	if len(o.Filters) == 0 {
		return fmt.Errorf("you must specify at least one tag filter")
	}
	return nil
}

func (o *GetAMIIDOptions) Run() error {
	currentNamespace, client, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("cannot obtain Kubernetes client: %v", err)
	}
	if len(o.Namespace) == 0 {
		o.Namespace = currentNamespace
	}
	id, err := o.GetAMIID(client)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", id)
	return nil
}

func (o *GetAMIIDOptions) GetAMIID(client clientset.Interface) (string, error) {
	ec2Client, err := getAWSClient(client, o.AccountSecretName, o.Namespace, o.Region)
	if err != nil {
		return "", fmt.Errorf("cannot obtain AWS client: %v", err)
	}
	describeImagesRequest := ec2.DescribeImagesInput{}
	for _, filter := range o.Filters {
		describeImagesRequest.Filters = append(describeImagesRequest.Filters, &ec2.Filter{
			Name:   aws.String(fmt.Sprintf("tag:%s", filter.Name)),
			Values: []*string{aws.String(filter.Value)},
		})
	}
	describeAMIResult, err := ec2Client.DescribeImages(&describeImagesRequest)
	if err != nil {
		return "", fmt.Errorf("error describing images: %v", err)
	}
	if len(describeAMIResult.Images) != 1 {
		return "", fmt.Errorf("unexpected number of images: %d", len(describeAMIResult.Images))
	}

	return *describeAMIResult.Images[0].ImageId, nil
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

func getAWSClient(kubeClient clientset.Interface, secretName, namespace, region string) (*ec2.EC2, error) {
	awsConfig := &aws.Config{Region: aws.String(region)}

	if secretName != "" {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(
			secretName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		accessKeyID, ok := secret.Data[awsCredsSecretIDKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				secretName, awsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[awsCredsSecretAccessKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				secretName, awsCredsSecretAccessKey)
		}

		awsConfig.Credentials = credentials.NewStaticCredentials(
			string(accessKeyID), string(secretAccessKey), "")
	}

	// Otherwise default to relying on the IAM role of the masters where the actuator is running:
	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}

	return ec2.New(s), nil
}

func parseFilter(str string) (*Filter, error) {
	parts := strings.SplitN(str, "=", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("incorrectly formatted filter")
	}
	return &Filter{
		Name:  parts[0],
		Value: parts[1],
	}, nil
}
