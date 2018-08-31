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
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-operator/contrib/pkg/amibuild"
	"github.com/openshift/cluster-operator/contrib/pkg/amiid"
)

func (o *ProvisionClusterOptions) SaveAMIName(name string, clients *clientContext) error {
	cmName := fmt.Sprintf("%s-aminame", o.Name)
	// Check if configmap exists and if it does, delete it first
	_, err := clients.KubeClient.CoreV1().ConfigMaps(o.Namespace).Get(cmName, metav1.GetOptions{})
	if err == nil {
		err = clients.KubeClient.CoreV1().ConfigMaps(o.Namespace).Delete(cmName, &metav1.DeleteOptions{})
		if err != nil {
			o.Logger.WithError(err).WithField("configmap", cmName).Error("failed to delete existing aminame configmap")
			return err
		}
	}
	cm := &corev1.ConfigMap{}
	cm.Name = cmName
	cm.Data = map[string]string{"aminame": name}
	_, err = clients.KubeClient.CoreV1().ConfigMaps(o.Namespace).Create(cm)
	return err
}

func (o *ProvisionClusterOptions) BuildAMI(name string, clients *clientContext) error {
	var err error
	if len(o.RPMRepositoryURL) == 0 {
		o.RPMRepositoryURL, err = o.determineRPMRepositoryURL(o.RPMRepositorySource)
		if err != nil {
			return fmt.Errorf("cannot retrieve RPM repository URL from %s: %v", o.RPMRepositorySource, err)
		}
		o.Logger.WithField("RepositoryURL", o.RPMRepositoryURL).Debug("Obtained RPM repository URL")
	}

	buildAMIOptions := amibuild.BuildAMIOptions{
		AccountSecretName:      o.AccountSecretName,
		SSHSecretName:          o.SSHSecretName,
		AnsibleImage:           o.AnsibleImage,
		AnsibleImagePullPolicy: corev1.PullIfNotPresent,
		RPMRepositoryURL:       o.RPMRepositoryURL,
		RPMRepositorySource:    o.RPMRepositorySource,
		Namespace:              o.Namespace,
		LogLevel:               o.LogLevel,
		VPCName:                o.AMIVPCName,
		Logger:                 o.Logger,
		Name:                   name,
	}

	o.Logger.Infof("Generating Build AMI pod and configmap")
	pod, cfgmap, err := buildAMIOptions.Generate()
	if err != nil {
		return err
	}
	o.Logger.Infof("Running Build AMI pod")
	err = buildAMIOptions.RunPod(clients.KubeClient, pod, cfgmap)
	if err != nil {
		return err
	}
	o.Logger.Infof("Determining AMI ID")
	getIdOptions := amiid.GetAMIIDOptions{
		Filters: []amiid.Filter{
			{
				Name:  "cluster-id",
				Value: name,
			},
		},
		Logger:            o.Logger,
		AccountSecretName: o.AccountSecretName,
		LogLevel:          o.LogLevel,
		Region:            o.Region,
		Namespace:         o.Namespace,
	}
	o.ImageID, err = getIdOptions.GetAMIID(clients.KubeClient)
	if err != nil {
		o.Logger.WithError(err).Warning("Received error retrieving AMI ID")
		return err
	}
	o.Logger.Infof("AMI ID is %s", o.ImageID)
	return nil
}

func (o *ProvisionClusterOptions) determineRPMRepositoryURL(sourceURL string) (string, error) {
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
