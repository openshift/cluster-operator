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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	clusterAPIGroupVersion         = "cluster.k8s.io/v1alpha1"
	clusterOperatorAPIGroupVersion = "clusteroperator.openshift.io/v1alpha1"
	sshPrivateKeyName              = "ssh-privatekey"
	sshPublicKeyName               = "ssh-publickey"
	awsAccessKeyId                 = "awsAccessKeyId"
	awsSecretAccessKey             = "awsSecretAccessKey"
)

func (o *ProvisionClusterOptions) PreflightChecks(clients *clientContext) error {
	if err := o.checkClusterAPI(clients.DiscoveryClient); err != nil {
		o.Logger.WithError(err).Error("Cluster API is not available")
		return err
	}
	if err := o.checkClusterOperatorAPI(clients.DiscoveryClient); err != nil {
		o.Logger.WithError(err).Error("Cluster Operator API is not available")
		return err
	}
	if err := o.checkSSHSecret(clients.KubeClient); err != nil {
		o.Logger.WithError(err).Error("SSH secret is not available")
		return err
	}
	if err := o.checkAccountSecret(clients.KubeClient); err != nil {
		o.Logger.WithError(err).Error("Account secret is not available")
		return err
	}
	return nil
}

func (o *ProvisionClusterOptions) checkClusterAPI(client discovery.DiscoveryInterface) error {
	_, err := client.ServerResourcesForGroupVersion(clusterAPIGroupVersion)
	return err
}

func (o *ProvisionClusterOptions) checkClusterOperatorAPI(client discovery.DiscoveryInterface) error {
	_, err := client.ServerResourcesForGroupVersion(clusterOperatorAPIGroupVersion)
	return err
}

func (o *ProvisionClusterOptions) checkSSHSecret(client clientset.Interface) error {
	secretName := fmt.Sprintf("%s/%s", o.Namespace, o.SSHSecretName)
	secret, err := client.CoreV1().Secrets(o.Namespace).Get(o.SSHSecretName, metav1.GetOptions{})
	if err != nil {
		o.Logger.WithError(err).WithField("secret", secretName).Error("cannot retrieve SSH secret")
		return err
	}
	if _, exists := secret.Data[sshPrivateKeyName]; !exists {
		o.Logger.WithField("secret", secretName).Errorf("does not have a key named %s", sshPrivateKeyName)
		return fmt.Errorf("missing secret key %s", sshPrivateKeyName)
	}
	if _, exists := secret.Data[sshPublicKeyName]; !exists {
		o.Logger.WithField("secret", secretName).Errorf("does not have a key named %s", sshPublicKeyName)
		return fmt.Errorf("missing secret key %s", sshPublicKeyName)
	}
	return nil
}

func (o *ProvisionClusterOptions) checkAccountSecret(client clientset.Interface) error {
	secretName := fmt.Sprintf("%s/%s", o.Namespace, o.AccountSecretName)
	secret, err := client.CoreV1().Secrets(o.Namespace).Get(o.AccountSecretName, metav1.GetOptions{})
	if err != nil {
		o.Logger.WithError(err).WithField("secret", secretName).Error("cannot retrieve account secret")
		return err
	}
	if _, exists := secret.Data[awsAccessKeyId]; !exists {
		o.Logger.WithField("secret", secretName).Errorf("does not have a key named %s", awsAccessKeyId)
		return fmt.Errorf("missing secret key %s", awsAccessKeyId)
	}
	if _, exists := secret.Data[awsSecretAccessKey]; !exists {
		o.Logger.WithField("secret", secretName).Errorf("does not have a key named %s", awsSecretAccessKey)
		return fmt.Errorf("missing secret key %s", awsSecretAccessKey)
	}
	return nil
}
