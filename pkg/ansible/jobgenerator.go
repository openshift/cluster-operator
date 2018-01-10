package ansible

import (
	"path"

	log "github.com/sirupsen/logrus"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	openshiftAnsibleContainerDir = "/usr/share/ansible/openshift-ansible/"
)

type JobGenerator interface {
	GeneratePlaybookJob(name string, hardware *clusteroperator.ClusterHardwareSpec, playbook, inventory, vars string) (*kbatch.Job, *kapi.ConfigMap)
}

type jobGenerator struct {
	image           string
	imagePullPolicy kapi.PullPolicy
}

func NewJobGenerator(openshiftAnsibleImage string, openshiftAnsibleImagePullPolicy kapi.PullPolicy) JobGenerator {
	return &jobGenerator{
		image:           openshiftAnsibleImage,
		imagePullPolicy: openshiftAnsibleImagePullPolicy,
	}
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

func (r *jobGenerator) GeneratePlaybookJob(name string, hardware *clusteroperator.ClusterHardwareSpec, playbook, inventory, vars string) (*kbatch.Job, *kapi.ConfigMap) {

	logger := log.WithField("playbook", playbook)

	logger.Infoln("generating ansible playbook job")

	cfgMap := r.generateInventoryConfigMap(name, inventory, vars, logger)

	playbookPath := path.Join(openshiftAnsibleContainerDir, playbook)
	env := []kapi.EnvVar{
		{
			Name:  "INVENTORY_FILE",
			Value: "/ansible/inventory/hosts",
		},
		{
			Name:  "PLAYBOOK_FILE",
			Value: playbookPath,
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

	// sshKeyFileMode is used to set the file permissions for the private SSH key
	sshKeyFileMode := int32(0600)

	podSpec := kapi.PodSpec{
		DNSPolicy:     kapi.DNSClusterFirst,
		RestartPolicy: kapi.RestartPolicyNever,

		Containers: []kapi.Container{
			{
				Name:            "ansible",
				Image:           r.image,
				ImagePullPolicy: r.imagePullPolicy,
				Env:             env,
				VolumeMounts: []kapi.VolumeMount{
					{
						Name:      "inventory",
						MountPath: "/ansible/inventory/",
					},
				},
			},
		},
		Volumes: []kapi.Volume{
			{
				// Mounts both our inventory and vars file.
				Name: "inventory",
				VolumeSource: kapi.VolumeSource{
					ConfigMap: &kapi.ConfigMapVolumeSource{
						LocalObjectReference: kapi.LocalObjectReference{
							Name: cfgMap.Name,
						},
					},
				},
			},
		},
	}

	if hardware.AWS != nil && len(hardware.AWS.SSHSecret.Name) > 0 {
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, kapi.VolumeMount{
			Name:      "sshkey",
			MountPath: "/ansible/ssh/",
		})
		podSpec.Volumes = append(podSpec.Volumes, kapi.Volume{
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

	completions := int32(1)
	deadline := int64(60 * 60) // one hour for now

	job := &kbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kbatch.JobSpec{
			Completions:           &completions,
			ActiveDeadlineSeconds: &deadline,
			Template: kapi.PodTemplateSpec{
				Spec: podSpec,
			},
		},
	}

	return job, cfgMap
}
