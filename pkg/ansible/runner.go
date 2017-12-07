package ansible

import (
	"fmt"
	"path"

	log "github.com/sirupsen/logrus"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	openshiftAnsibleImage          = "openshift/origin-ansible:v3.7"
	openshiftAnsibleServiceAccount = "cluster-operator-controller-manager"
	sshPrivateKeySecret            = "ssh-private-key"
	operatorNamespace              = "cluster-operator"
	openshiftAnsibleContainerDir   = "/usr/share/ansible/openshift-ansible/"
	awsCredentialsSecret           = "aws-credentials"
)

type ansibleRunner struct {
	KubeClient kubernetes.Interface
	Image      string
}

func NewAnsibleRunner(kubeClient kubernetes.Interface) *ansibleRunner {
	return &ansibleRunner{
		KubeClient: kubeClient,
		Image:      openshiftAnsibleImage,
	}
}
func (r *ansibleRunner) createInventoryConfigMap(clusterName, jobPrefix, inventory, vars string) (*kapi.ConfigMap, error) {
	log.Debugln("Creating inventory/vars ConfigMap")
	genNamePrefix := fmt.Sprintf("%s%s-", jobPrefix, clusterName)
	cfgmap := &kapi.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genNamePrefix,
		},
		Data: map[string]string{
			"hosts": inventory,
			"vars":  vars,
		},
	}
	cfgMap, err := r.KubeClient.CoreV1().ConfigMaps(operatorNamespace).Create(cfgmap)
	if err != nil {
		return cfgMap, err
	}
	log.WithField("configmap", cfgMap.Name).Infoln("created successfully")

	return cfgMap, err
}

func (r *ansibleRunner) RunPlaybook(clusterName, jobPrefix, playbook, inventory, vars string) error {
	logger := log.WithFields(log.Fields{
		"cluster":  clusterName,
		"playbook": playbook,
	})
	logger.Infoln("running ansible playbook")

	// TODO: cleanup configmaps when their job is deleted
	cfgMap, err := r.createInventoryConfigMap(clusterName, jobPrefix, inventory, vars)
	if err != nil {
		return err
	}

	env := []kapi.EnvVar{
		{
			Name:  "INVENTORY_FILE",
			Value: "/ansible/inventory/hosts",
		},
		{
			Name:  "PLAYBOOK_FILE",
			Value: playbook,
		},
		{
			Name:  "ANSIBLE_HOST_KEY_CHECKING",
			Value: "False",
		},
		{
			Name:  "OPTS",
			Value: "-vvv --private-key=/ansible/ssh/privatekey.pem -e @/ansible/inventory/vars",
		},
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &kapi.EnvVarSource{
				SecretKeyRef: &kapi.SecretKeySelector{
					LocalObjectReference: kapi.LocalObjectReference{Name: awsCredentialsSecret},
					Key:                  "aws_access_key_id",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &kapi.EnvVarSource{
				SecretKeyRef: &kapi.SecretKeySelector{
					LocalObjectReference: kapi.LocalObjectReference{Name: awsCredentialsSecret},
					Key:                  "aws_secret_access_key",
				},
			},
		},
	}
	runAsUser := int64(0)
	sshKeyFileMode := int32(0600)
	playbookPath := path.Join(openshiftAnsibleContainerDir, playbook)
	podSpec := kapi.PodSpec{
		DNSPolicy:          kapi.DNSClusterFirst,
		RestartPolicy:      kapi.RestartPolicyNever,
		ServiceAccountName: openshiftAnsibleServiceAccount,

		Containers: []kapi.Container{
			{
				Name:  "ansible",
				Image: r.Image,
				Env:   env,
				SecurityContext: &kapi.SecurityContext{
					RunAsUser: &runAsUser,
				},
				VolumeMounts: []kapi.VolumeMount{
					{
						Name:      "inventory",
						MountPath: "/ansible/inventory/",
					},
					{
						Name:      "sshkey",
						MountPath: "/ansible/ssh/",
					},
				},

				// TODO: possibly drop this once https://github.com/openshift/openshift-ansible/pull/6320 merges, the default run script should then work, minus the vars file :(
				//Command: []string{"ansible-playbook", "-i", "/ansible/inventory/hosts", "--private-key", "/ansible/ssh/privatekey.pem", playbookPath, "-e", "@/ansible/inventory/vars"},

				// TODO: committing as no-op for now, uncomment command above to start testing provisioning
				Command: []string{"echo", playbookPath},
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
			{
				Name: "sshkey",
				VolumeSource: kapi.VolumeSource{
					Secret: &kapi.SecretVolumeSource{
						SecretName: sshPrivateKeySecret,
						Items: []kapi.KeyToPath{
							{
								Key:  "ssh-privatekey",
								Path: "privatekey.pem",
								Mode: &sshKeyFileMode,
							},
						},
					},
				},
			},
		},
	}

	completions := int32(1)
	deadline := int64(60 * 60) // one hour for now

	meta := metav1.ObjectMeta{
		// Re-use configmaps random name so we can easily correlate the two.
		Name: cfgMap.Name,
	}

	job := &kbatch.Job{
		ObjectMeta: meta,
		Spec: kbatch.JobSpec{
			Completions:           &completions,
			ActiveDeadlineSeconds: &deadline,
			Template: kapi.PodTemplateSpec{
				Spec: podSpec,
			},
		},
	}

	// Create the job client
	jobClient := r.KubeClient.Batch().Jobs(operatorNamespace)

	// Submit the job
	job, err = jobClient.Create(job)
	if err != nil {
		return err
	}

	logger.WithField("job", job.Name).Infoln("job created")
	return nil
}
