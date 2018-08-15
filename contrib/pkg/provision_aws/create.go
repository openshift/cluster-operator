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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateArtifacts creates the artifacts related to a new cluster deployment
func (o *ProvisionClusterOptions) CreateArtifacts(clients *clientContext, artifacts *ClusterArtifacts) error {
	if _, err := clients.KubeClient.CoreV1().Secrets(o.Namespace).Get(artifacts.SSLSecret.Name, metav1.GetOptions{}); err == nil {
		err = clients.KubeClient.CoreV1().Secrets(o.Namespace).Delete(artifacts.SSLSecret.Name, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	_, err := clients.KubeClient.CoreV1().Secrets(o.Namespace).Create(artifacts.SSLSecret)
	if err != nil {
		return err
	}

	if _, err := clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterVersions(o.Namespace).Get(artifacts.ClusterVersion.Name, metav1.GetOptions{}); err == nil {
		err = clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterVersions(o.Namespace).Delete(artifacts.ClusterVersion.Name, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	_, err = clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterVersions(o.Namespace).Create(artifacts.ClusterVersion)
	if err != nil {
		return err
	}

	clusterDeployment, err := clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterDeployments(o.Namespace).Create(artifacts.ClusterDeployment)
	if err != nil {
		return err
	}
	o.ClusterName = clusterDeployment.Spec.ClusterName
	return nil
}
