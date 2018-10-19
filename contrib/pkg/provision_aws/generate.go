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

	corev1 "k8s.io/api/core/v1"

	coapiv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	defaultClusterOperatorImage = "quay.io/twiest/cluster-operator:latest"
	defaultInstanceType         = "m4.xlarge"
	defaultSSHUser              = "centos"
)

type ClusterArtifacts struct {
	SSLSecret         *corev1.Secret
	ClusterDeployment *coapiv1.ClusterDeployment
	ClusterVersion    *coapiv1.ClusterVersion
}

func (o *ProvisionClusterOptions) GenerateClusterArtifacts(amiName string, servingCert *ServingCert) *ClusterArtifacts {
	result := &ClusterArtifacts{}
	sslSecretName := fmt.Sprintf("%s-cert", o.Name)
	s := &corev1.Secret{}
	s.Name = sslSecretName
	s.Data = map[string][]byte{
		"server.crt": servingCert.Cert,
		"server.key": servingCert.Key,
	}
	result.SSLSecret = s

	v := &coapiv1.ClusterVersion{}
	clusterOperatorImage := defaultClusterOperatorImage
	v.Name = fmt.Sprintf("%s-version", o.Name)
	v.Spec.Images.ImageFormat = o.ImageFormat
	v.Spec.Images.OpenshiftAnsibleImage = &o.AnsibleImage
	pullIfNotPresent := corev1.PullIfNotPresent
	pullAlways := corev1.PullAlways
	v.Spec.Images.OpenshiftAnsibleImagePullPolicy = &pullIfNotPresent
	v.Spec.Images.ClusterAPIImage = &clusterOperatorImage
	v.Spec.Images.ClusterAPIImagePullPolicy = &pullAlways
	v.Spec.Images.MachineControllerImage = &clusterOperatorImage
	v.Spec.Images.MachineControllerImagePullPolicy = &pullAlways
	v.Spec.Images.EtcdImage = o.EtcdImage
	v.Spec.VMImages.AWSImages = &coapiv1.AWSVMImages{
		RegionAMIs: []coapiv1.AWSRegionAMIs{
			{
				Region: o.Region,
				AMI:    o.ImageID,
			},
		},
	}
	v.Spec.DeploymentType = coapiv1.ClusterDeploymentTypeOrigin
	v.Spec.Version = o.Version
	result.ClusterVersion = v

	cd := &coapiv1.ClusterDeployment{}
	cd.Name = o.Name
	cd.Annotations = map[string]string{
		"clusteroperator.openshift.io/s3-backed-registry": "false",
		"clusteroperator.openshift.io/ami-id":             o.ImageID,
	}
	if len(amiName) > 0 {
		cd.Annotations["clusteroperator.openshift.io/ami-name"] = amiName
	}
	cd.Spec.Hardware.AWS = &coapiv1.AWSClusterSpec{
		Defaults: &coapiv1.MachineSetAWSHardwareSpec{
			InstanceType: defaultInstanceType,
		},
		AccountSecret: corev1.LocalObjectReference{
			Name: o.AccountSecretName,
		},
		SSHSecret: corev1.LocalObjectReference{
			Name: o.SSHSecretName,
		},
		SSHUser: defaultSSHUser,
		SSLSecret: corev1.LocalObjectReference{
			Name: sslSecretName,
		},
		KeyPairName: o.KeyPairName,
		Region:      o.Region,
		VPCName:     o.VPCName,
	}
	cd.Spec.DefaultHardwareSpec = &coapiv1.MachineSetHardwareSpec{
		AWS: &coapiv1.MachineSetAWSHardwareSpec{
			InstanceType: defaultInstanceType,
		},
	}
	cd.Spec.MachineSets = []coapiv1.ClusterMachineSet{
		{
			ShortName: "",
			MachineSetConfig: coapiv1.MachineSetConfig{
				NodeType: coapiv1.NodeTypeMaster,
				Size:     1,
			},
		},
		{
			ShortName: "infra",
			MachineSetConfig: coapiv1.MachineSetConfig{
				Infra:    true,
				NodeType: coapiv1.NodeTypeCompute,
				Size:     1,
			},
		},
		{
			ShortName: "compute",
			MachineSetConfig: coapiv1.MachineSetConfig{
				NodeType: coapiv1.NodeTypeCompute,
				Size:     1,
			},
		},
	}
	cd.Spec.ClusterVersionRef = coapiv1.ClusterVersionReference{
		Namespace: o.Namespace,
		Name:      fmt.Sprintf("%s-version", o.Name),
	}
	result.ClusterDeployment = cd

	return result
}
