package ansible

import (
	"fmt"
	"path"

	log "github.com/sirupsen/logrus"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	openshiftAnsibleImage        = "openshift/origin-ansible:v3.8"
	operatorNamespace            = "cluster-operator"
	openshiftAnsibleContainerDir = "/usr/share/ansible/openshift-ansible/"
	sshKeySecretName             = "ssh-private-key"
	awsCredsSecretName           = "aws-credentials"
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
func (r *ansibleRunner) createInventoryConfigMap(namespace, clusterName, jobPrefix, inventory, vars string) (*kapi.ConfigMap, error) {
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
	cfgMap, err := r.KubeClient.CoreV1().ConfigMaps(namespace).Create(cfgmap)
	if err != nil {
		return cfgMap, err
	}
	log.WithField("configmap", cfgMap.Name).Infoln("created successfully")

	return cfgMap, err
}

// copySecrets copies the AWS credentials and SSH key secrets from the cluster-operator
// namespace, to the namespace where the cluster is being created. This allows us to
// run ansible jobs/pods in the cluster's namespace and keep the data together. This is temporary,
// eventually the AWS credentials secret will be provided before creating the cluster,
// and the SSH key may be generated.
func (r *ansibleRunner) copySecrets(namespace string) error {
	awsSecret, err := r.KubeClient.CoreV1().Secrets(operatorNamespace).Get(awsCredsSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	sshKeySecret, err := r.KubeClient.CoreV1().Secrets(operatorNamespace).Get(sshKeySecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if namespace == operatorNamespace {
		// No need to copy secrets, we expect them to already exist in this ns. We let the above
		// run to make sure they exist however.
		return nil
	}

	destAwsSecret := &kapi.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: awsCredsSecretName,
		},
		Data: awsSecret.Data,
		Type: awsSecret.Type,
	}
	_, err = r.KubeClient.CoreV1().Secrets(namespace).Create(destAwsSecret)
	if err != nil {
		if kapierrors.IsAlreadyExists(err) {
			log.WithFields(log.Fields{
				"secret":    awsCredsSecretName,
				"namespace": namespace,
			}).Debugln("secret already exists, skipping copy")
		} else {
			return err
		}
	}

	log.WithFields(log.Fields{
		"secret":    awsCredsSecretName,
		"namespace": namespace,
	}).Debugln("secret copied")

	destSshSecret := &kapi.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: sshKeySecretName,
		},
		Data: sshKeySecret.Data,
		Type: sshKeySecret.Type,
	}
	_, err = r.KubeClient.CoreV1().Secrets(namespace).Create(destSshSecret)
	if err != nil {
		if kapierrors.IsAlreadyExists(err) {
			log.WithFields(log.Fields{
				"secret":    sshKeySecretName,
				"namespace": namespace,
			}).Debugln("secret already exists, skipping copy")
		} else {
			return err
		}
	}
	log.WithFields(log.Fields{
		"secret":    sshKeySecretName,
		"namespace": namespace,
	}).Debugln("secret copied")

	return nil
}

func (r *ansibleRunner) RunPlaybook(namespace, clusterName, jobPrefix, playbook,
	inventory, vars string) error {

	logger := log.WithFields(log.Fields{
		"cluster":  clusterName,
		"playbook": playbook,
	})
	logger.Infoln("running ansible playbook")

	err := r.copySecrets(namespace)
	if err != nil {
		return err
	}

	// TODO: cleanup configmaps when their job is deleted
	cfgMap, err := r.createInventoryConfigMap(namespace, clusterName, jobPrefix, inventory, vars)
	if err != nil {
		return err
	}

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
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &kapi.EnvVarSource{
				SecretKeyRef: &kapi.SecretKeySelector{
					LocalObjectReference: kapi.LocalObjectReference{Name: awsCredsSecretName},
					Key:                  "aws_access_key_id",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &kapi.EnvVarSource{
				SecretKeyRef: &kapi.SecretKeySelector{
					LocalObjectReference: kapi.LocalObjectReference{Name: awsCredsSecretName},
					Key:                  "aws_secret_access_key",
				},
			},
		},
	}
	sshKeyFileMode := int32(0600)
	podSpec := kapi.PodSpec{
		DNSPolicy:     kapi.DNSClusterFirst,
		RestartPolicy: kapi.RestartPolicyNever,

		Containers: []kapi.Container{
			{
				Name:  "ansible",
				Image: r.Image,
				Env:   env,
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

				// TODO: committing as no-op for now, comment this out to use the default
				// origin-ansible container entrypoint and actually run playbooks.
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
						SecretName: sshKeySecretName,
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
	jobClient := r.KubeClient.Batch().Jobs(namespace)

	// Submit the job
	job, err = jobClient.Create(job)
	if err != nil {
		return err
	}

	logger.WithField("job", job.Name).Infoln("job created")
	return nil
}
