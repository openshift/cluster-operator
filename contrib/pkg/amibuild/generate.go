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
	"bytes"
	"path"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/storage/names"
)

const (
	openshiftAnsibleContainerDir = "/usr/share/ansible/openshift-ansible/"
	buildAMIPlaybook             = "playbooks/aws/openshift-cluster/build_ami.yml"
)

const inventory = `[OSEv3:children]
masters
nodes
etcd

[OSEv3:vars]
ansible_become=true

[masters]

[etcd]

[nodes]
`

const varsTemplate = `---
all:
  vars:
    openshift_aws_base_ami: ami-b81dbfc5
    openshift_aws_base_ami_name: [[ .Name ]]
    openshift_aws_build_ami_group: default
    openshift_aws_clusterid: [[ .Name ]]
    openshift_aws_region: us-east-1
    openshift_aws_create_vpc: false
    openshift_aws_vpc_name: [[ .VPCName ]]
    openshift_aws_ssh_key_name: libra
    ansible_ssh_user: centos
    openshift_deployment_type: origin
    openshift_aws_create_security_groups: false
    openshift_additional_repos: [{'id': 'origin-latest', 'name': 'origin-latest', 'baseurl': '[[ .RPMRepositoryURL ]]', 'enabled': 1, 'gpgcheck': 0}]
    openshift_aws_ami_tags:
      bootstrap: "true"
      openshift-created: "true"
      parent: "{{ openshift_aws_base_ami | default('unknown') }}"
      cluster-id: [[ .Name ]]
`

func (g *BuildAMIOptions) Generate() (*corev1.Pod, *corev1.ConfigMap, error) {

	g.Logger.Infoln("generating ansible playbook pod")

	if len(g.Name) == 0 {
		g.Name = names.SimpleNameGenerator.GenerateName("buildami-")
	}

	vars, err := g.generateVars(g.Name)
	if err != nil {
		return nil, nil, err
	}

	cfgMap := g.generateInventoryConfigMap(g.Name, inventory, vars)

	env := []corev1.EnvVar{
		{
			Name:  "INVENTORY_DIR",
			Value: "/ansible/inventory",
		},
		{
			Name:  "ANSIBLE_HOST_KEY_CHECKING",
			Value: "False",
		},
		{
			Name:  "OPTS",
			Value: "-vvv --private-key=/ansible/ssh/privatekey.pem",
		},
	}

	env = append(env, []corev1.EnvVar{
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: g.AccountSecretName},
					Key:                  "awsAccessKeyId",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: g.AccountSecretName},
					Key:                  "awsSecretAccessKey",
				},
			},
		},
	}...)

	volumes := make([]corev1.Volume, 0, 2)
	volumeMounts := make([]corev1.VolumeMount, 0, 2)

	// Mounts for inventory and vars files
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "inventory",
		MountPath: "/ansible/inventory/",
	})
	volumes = append(volumes, corev1.Volume{
		Name: "inventory",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cfgMap.Name,
				},
			},
		},
	})

	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "sshkey",
		MountPath: "/ansible/ssh/",
	})
	// sshKeyFileMode is used to set the file permissions for the private SSH key
	sshKeyFileMode := int32(0660)
	volumes = append(volumes, corev1.Volume{
		Name: "sshkey",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: g.SSHSecretName,
				Items: []corev1.KeyToPath{
					{
						Key:  "ssh-privatekey",
						Path: "privatekey.pem",
						Mode: &sshKeyFileMode,
					},
				},
			},
		},
	})

	playbookPath := path.Join(openshiftAnsibleContainerDir, buildAMIPlaybook)
	env = append(env, corev1.EnvVar{
		Name:  "PLAYBOOK_FILE",
		Value: playbookPath,
	})
	containers := []corev1.Container{
		{
			Name:            "buildami",
			Image:           g.AnsibleImage,
			ImagePullPolicy: corev1.PullPolicy(g.AnsibleImagePullPolicy),
			Env:             env,
			VolumeMounts:    volumeMounts,
		},
	}

	podSpec := corev1.PodSpec{
		DNSPolicy:     corev1.DNSClusterFirst,
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    containers,
		Volumes:       volumes,
	}

	pod := &corev1.Pod{}
	pod.Name = g.Name
	pod.Spec = podSpec

	return pod, cfgMap, nil
}

func (g *BuildAMIOptions) generateInventoryConfigMap(name, inventory, vars string) *corev1.ConfigMap {
	g.Logger.Debugln("Generating inventory/vars ConfigMap")
	cfgMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string]string{
			"hosts": inventory,
			"vars":  vars,
		},
	}
	g.Logger.Infoln("configmap generated")
	return cfgMap
}

func (g *BuildAMIOptions) generateVars(name string) (string, error) {
	t, err := template.New("vars").Delims("[[", "]]").Parse(varsTemplate)
	if err != nil {
		return "", err
	}
	params := &struct {
		Name             string
		RPMRepositoryURL string
		VPCName          string
	}{
		Name:             name,
		RPMRepositoryURL: g.RPMRepositoryURL,
		VPCName:          g.VPCName,
	}
	buf := &bytes.Buffer{}
	err = t.Execute(buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
