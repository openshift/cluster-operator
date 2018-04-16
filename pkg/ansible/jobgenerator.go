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

package ansible

import (
	"path"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	openshiftAnsibleContainerDir = "/usr/share/ansible/openshift-ansible/"
)

// JobGenerator is used to generate jobs that run ansible playbooks.
type JobGenerator interface {
	// GeneratePlaybookJob generates a job to run the specified playbook.
	// Note that neither the job nor the configmap will be created in the API
	// server. It is the responsibility of the caller to create the objects
	// in the API server.
	//
	// name - name to give the job and the configmap
	// hardware - details of the hardware of the target cluster
	// playbook - name of the playbook to run
	// inventory - inventory to pass to the playbook
	// vars - Ansible variables to the pass to the playbook
	// openshiftAnsibleImage - name of the openshift-ansible image that the
	//   jobs created by the job generator will use
	// openshiftAnsibleImagePullPolicy - policy to use to pull the
	//   openshift-ansible image
	GeneratePlaybookJob(
		name string,
		hardware *clusteroperator.ClusterHardwareSpec,
		playbook string,
		inventory string,
		vars string,
		openshiftAnsibleImage string,
		openshiftAnsibleImagePullPolicy kapi.PullPolicy,
	) (*kbatch.Job, *kapi.ConfigMap)

	// GeneratePlaybookJobWithServiceAccount is same as GeneratePlaybookJob
	// but allows passing in a serviceaccount object
	// serviceAccount - ServiceAccount object to apply to job/pod
	GeneratePlaybookJobWithServiceAccount(
		name string,
		hardware *clusteroperator.ClusterHardwareSpec,
		playbook string,
		inventory string,
		vars string,
		openshiftAnsibleImage string,
		openshiftAnsibleImagePullPolicy kapi.PullPolicy,
		serviceAccount *kapi.ServiceAccount,
	) (*kbatch.Job, *kapi.ConfigMap)

	// GeneratePlaybooksJob generates a job to run the specified playbooks.
	// Note that neither the job nor the configmap will be created in the API
	// server. It is the responsibility of the caller to create the objects
	// in the API server.
	//
	// name - name to give the job and the configmap
	// hardware - details of the hardware of the target cluster
	// playbooks - names of the playbooks to run
	// inventory - inventory to pass to the playbook
	// vars - Ansible variables to the pass to the playbook
	// openshiftAnsibleImage - name of the openshift-ansible image that the
	//   jobs created by the job generator will use
	// openshiftAnsibleImagePullPolicy - policy to use to pull the
	//   openshift-ansible image
	GeneratePlaybooksJob(
		name string,
		hardware *clusteroperator.ClusterHardwareSpec,
		playbooks []string,
		inventory,
		vars,
		openshiftAnsibleImage string,
		openshiftAnsibleImagePullPolicy kapi.PullPolicy) (*kbatch.Job, *kapi.ConfigMap)

	// GeneratePlaybooksJobWithServiceAccount generates a job to run the specified playbooks.
	// Note that neither the job nor the configmap will be created in the API
	// server. It is the responsibility of the caller to create the objects
	// in the API server.
	//
	// name - name to give the job and the configmap
	// hardware - details of the hardware of the target cluster
	// playbooks - names of the playbooks to run
	// inventory - inventory to pass to the playbook
	// vars - Ansible variables to the pass to the playbook
	// openshiftAnsibleImage - name of the openshift-ansible image that the
	//   jobs created by the job generator will use
	// openshiftAnsibleImagePullPolicy - policy to use to pull the
	//   openshift-ansible image
	// serviceAccount - (optional) serviceaccount object to attach to the job/pod
	GeneratePlaybooksJobWithServiceAccount(
		name string,
		hardware *clusteroperator.ClusterHardwareSpec,
		playbooks []string,
		inventory string,
		vars string,
		openshiftAnsibleImage string,
		openshiftAnsibleImagePullPolicy kapi.PullPolicy,
		serviceAccount *kapi.ServiceAccount,
	) (*kbatch.Job, *kapi.ConfigMap)
}

type jobGenerator struct {
}

// NewJobGenerator creates a new JobGenerator that can be used to create
// Ansible jobs.
//
func NewJobGenerator() JobGenerator {
	return &jobGenerator{}
}

func (r *jobGenerator) generateInventoryConfigMap(name, inventory, vars string, logger *log.Entry) *kapi.ConfigMap {
	logger.Debugln("Generating inventory/vars ConfigMap")
	cfgMap := &kapi.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string]string{
			"hosts": inventory,
			"vars":  vars,
		},
	}
	logger.Infoln("configmap generated")

	return cfgMap
}

func (r *jobGenerator) GeneratePlaybookJob(name string, hardware *clusteroperator.ClusterHardwareSpec, playbook, inventory, vars, openshiftAnsibleImage string, openshiftAnsibleImagePullPolicy kapi.PullPolicy) (*kbatch.Job, *kapi.ConfigMap) {
	return r.GeneratePlaybooksJobWithServiceAccount(name, hardware, []string{playbook},
		inventory, vars, openshiftAnsibleImage, openshiftAnsibleImagePullPolicy, nil)
}

func (r *jobGenerator) GeneratePlaybookJobWithServiceAccount(
	name string,
	hardware *clusteroperator.ClusterHardwareSpec,
	playbook string,
	inventory,
	vars,
	openshiftAnsibleImage string,
	openshiftAnsibleImagePullPolicy kapi.PullPolicy,
	serviceAccount *kapi.ServiceAccount) (*kbatch.Job, *kapi.ConfigMap) {
	return r.GeneratePlaybooksJobWithServiceAccount(name, hardware, []string{playbook}, inventory, vars,
		openshiftAnsibleImage, openshiftAnsibleImagePullPolicy, serviceAccount)
}

func (r *jobGenerator) GeneratePlaybooksJob(
	name string,
	hardware *clusteroperator.ClusterHardwareSpec,
	playbooks []string,
	inventory,
	vars,
	openshiftAnsibleImage string,
	openshiftAnsibleImagePullPolicy kapi.PullPolicy) (*kbatch.Job, *kapi.ConfigMap) {
	return r.GeneratePlaybooksJobWithServiceAccount(name, hardware, playbooks, inventory, vars,
		openshiftAnsibleImage, openshiftAnsibleImagePullPolicy, nil)
}

func (r *jobGenerator) GeneratePlaybooksJobWithServiceAccount(
	name string,
	hardware *clusteroperator.ClusterHardwareSpec,
	playbooks []string,
	inventory,
	vars,
	openshiftAnsibleImage string,
	openshiftAnsibleImagePullPolicy kapi.PullPolicy,
	serviceAccount *kapi.ServiceAccount) (*kbatch.Job, *kapi.ConfigMap) {

	logger := log.WithField("playbooks", playbooks)

	logger.Infoln("generating ansible playbooks job")

	cfgMap := r.generateInventoryConfigMap(name, inventory, vars, logger)

	env := []kapi.EnvVar{
		{
			Name:  "INVENTORY_FILE",
			Value: "/ansible/inventory/hosts",
		},
		{
			Name:  "ANSIBLE_HOST_KEY_CHECKING",
			Value: "False",
		},
		{
			Name:  "OPTS",
			Value: "-vvv --private-key=/ansible/ssh/privatekey.pem -e @/ansible/inventory/vars",
		},
	}

	if hardware.AWS != nil && len(hardware.AWS.AccountSecret.Name) > 0 {
		env = append(env, []kapi.EnvVar{
			{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &kapi.EnvVarSource{
					SecretKeyRef: &kapi.SecretKeySelector{
						LocalObjectReference: hardware.AWS.AccountSecret,
						Key:                  "aws_access_key_id",
					},
				},
			},
			{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &kapi.EnvVarSource{
					SecretKeyRef: &kapi.SecretKeySelector{
						LocalObjectReference: hardware.AWS.AccountSecret,
						Key:                  "aws_secret_access_key",
					},
				},
			},
		}...)
	}

	volumes := make([]kapi.Volume, 0, 3)
	volumeMounts := make([]kapi.VolumeMount, 0, 3)

	// Mounts for inventory and vars files
	volumeMounts = append(volumeMounts, kapi.VolumeMount{
		Name:      "inventory",
		MountPath: "/ansible/inventory/",
	})
	volumes = append(volumes, kapi.Volume{
		Name: "inventory",
		VolumeSource: kapi.VolumeSource{
			ConfigMap: &kapi.ConfigMapVolumeSource{
				LocalObjectReference: kapi.LocalObjectReference{
					Name: cfgMap.Name,
				},
			},
		},
	})

	if hardware.AWS != nil {
		if len(hardware.AWS.SSHSecret.Name) > 0 {
			volumeMounts = append(volumeMounts, kapi.VolumeMount{
				Name:      "sshkey",
				MountPath: "/ansible/ssh/",
			})
			// sshKeyFileMode is used to set the file permissions for the private SSH key
			sshKeyFileMode := int32(0600)
			volumes = append(volumes, kapi.Volume{
				Name: "sshkey",
				VolumeSource: kapi.VolumeSource{
					Secret: &kapi.SecretVolumeSource{
						SecretName: hardware.AWS.SSHSecret.Name,
						Items: []kapi.KeyToPath{
							{
								Key:  "ssh-privatekey",
								Path: "privatekey.pem",
								Mode: &sshKeyFileMode,
							},
						},
					},
				},
			})
		}
		if len(hardware.AWS.SSLSecret.Name) > 0 {
			volumeMounts = append(volumeMounts, kapi.VolumeMount{
				Name:      "sslkey",
				MountPath: "/ansible/ssl/",
			})
			volumes = append(volumes, kapi.Volume{
				Name: "sslkey",
				VolumeSource: kapi.VolumeSource{
					Secret: &kapi.SecretVolumeSource{
						SecretName: hardware.AWS.SSLSecret.Name,
					},
				},
			})
		}
	}

	containers := make([]kapi.Container, len(playbooks))
	for i, playbook := range playbooks {
		playbookPath := path.Join(openshiftAnsibleContainerDir, playbook)
		envForPlaybook := make([]kapi.EnvVar, len(env)+1)
		copy(envForPlaybook, env)
		envForPlaybook[len(env)] = kapi.EnvVar{
			Name:  "PLAYBOOK_FILE",
			Value: playbookPath,
		}
		containers[i] = kapi.Container{
			Name:            containerNameForPlaybook(playbook),
			Image:           openshiftAnsibleImage,
			ImagePullPolicy: openshiftAnsibleImagePullPolicy,
			Env:             envForPlaybook,
			VolumeMounts:    volumeMounts,
		}
	}

	podSpec := kapi.PodSpec{
		DNSPolicy:     kapi.DNSClusterFirst,
		RestartPolicy: kapi.RestartPolicyOnFailure,
		Containers:    containers,
		Volumes:       volumes,
	}

	if serviceAccount != nil {
		podSpec.ServiceAccountName = serviceAccount.Name
	}

	completions := int32(1)
	deadline := int64((24 * time.Hour).Seconds())
	backoffLimit := int32(123456) // effectively limitless

	job := &kbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kbatch.JobSpec{
			Completions:           &completions,
			ActiveDeadlineSeconds: &deadline,
			BackoffLimit:          &backoffLimit,
			Template: kapi.PodTemplateSpec{
				Spec: podSpec,
			},
		},
	}

	return job, cfgMap
}

func containerNameForPlaybook(playbook string) string {
	_, filename := path.Split(playbook)
	extensionStart := strings.LastIndexByte(filename, '.')
	containerName := filename
	if extensionStart >= 0 {
		containerName = filename[0:extensionStart]
	}
	replaceInvalidRunes := func(r rune) rune {
		switch {
		case 'A' <= r && r <= 'Z':
			return r
		case 'a' <= r && r <= 'z':
			return r
		case '0' <= r && r <= '9':
			return r
		case r == '-':
			return r
		default:
			return '-'
		}
	}
	return strings.Map(replaceInvalidRunes, containerName)
}
