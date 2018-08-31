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
	"io/ioutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (o *ProvisionClusterOptions) SaveKubeconfig(clients *clientContext) error {
	if len(o.KubeconfigFile) == 0 {
		return nil
	}

	secret, err := clients.KubeClient.CoreV1().Secrets(o.Namespace).Get(fmt.Sprintf("%s-kubeconfig", o.ClusterName), metav1.GetOptions{})
	if err != nil {
		return err
	}

	kubeconfig, present := secret.Data["kubeconfig"]
	if !present {
		return fmt.Errorf("Cannot find kubeconfig in secret")
	}

	return ioutil.WriteFile(o.KubeconfigFile, kubeconfig, 0644)
}
