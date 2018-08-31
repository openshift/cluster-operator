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

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	kbatch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

// completeProcessingJob completes a processing job for the object with the
// specified namespace and name. Verification is performed to wait for the
// object to be in a state where the job is running before completing the job
// and to wait for the object to be in a state where the job has been
// completed successfully after the job is completed.
func completeProcessingJob(
	t *testing.T,
	jobLabel string,
	kubeClient *kubefake.Clientset,
	namespace, name string,
	getFromStore func(namespace, name string) (metav1.Object, error),
	getJobRef func(metav1.Object) *corev1.LocalObjectReference,
	verifyJobCompleted func(namespace, name string) error,
) bool {
	if err := waitForObjectStatus(
		namespace, name,
		getFromStore,
		func(obj metav1.Object) bool {
			// Wait for a reference to a job that has not been completed.
			// If the job has been completed already, then it is a job for a
			// previous processing. For example, when deprovisioning the job
			// reference may temporarily refer to the provisioning job until the
			// controller creates the deprovisioning job.
			jobRef := getJobRef(obj)
			if jobRef == nil {
				return false
			}
			job, err := kubeClient.BatchV1().Jobs(namespace).Get(jobRef.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return !isJobComplete(job)
		},
	); err != nil {
		t.Fatalf("error waiting for %s job creation for object %s/%s: %v", jobLabel, namespace, name, err)
	}

	obj, err := getFromStore(namespace, name)
	if !assert.NoError(t, err, "error getting object %s/%s for %s job", namespace, name, jobLabel) {
		return false
	}
	if !assert.NotNil(t, obj, "expecting object %s/%s to exist for %s job", namespace, name, jobLabel) {
		return false
	}
	jobRef := getJobRef(obj)
	if !assert.NotNil(t, jobRef, "expecting object %s/%s to be associated with a %s job", namespace, name, jobLabel) {
		return false
	}
	job, err := getJob(kubeClient, namespace, jobRef.Name)
	if !assert.NoError(t, err, "error getting %s job for object %s/%s", jobLabel, namespace, name) {
		return false
	}
	if !assert.NotNil(t, job, "expecting %s job to exist for object %s/%s", jobLabel, namespace, name) {
		return false
	}

	if err := completeJob(kubeClient, job); err != nil {
		t.Fatalf("error updating %s job for object %s/%s: %v", jobLabel, namespace, name, err)
	}

	if err := verifyJobCompleted(namespace, name); err != nil {
		t.Fatalf("error waiting for %s job completion for object %s/%s: %v", jobLabel, namespace, name, err)
	}

	return true
}

// completeClusterProcessingJob completes a processing job for the cluster
// with the specified namespace and name. Verification is performed to wait
// for the cluster to be in a state where the job is running before completing
// the job and to wait for the cluster to be in a state where the job has been
// completed successfully after the job is completed.
func completeClusterProcessingJob(
	t *testing.T,
	jobLabel string,
	kubeClient *kubefake.Clientset,
	capiClient capiclientset.Interface,
	namespace, name string,
	getJobRef func(*capiv1.Cluster) *corev1.LocalObjectReference,
	verifyJobCompleted func(capiclientset.Interface /*namespace*/, string /*name*/, string) error,
) bool {
	return completeProcessingJob(
		t,
		jobLabel,
		kubeClient,
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			return getCluster(capiClient, namespace, name)
		},
		func(obj metav1.Object) *corev1.LocalObjectReference {
			return getJobRef(obj.(*capiv1.Cluster))
		},
		func(namespace, name string) error {
			return verifyJobCompleted(capiClient, namespace, name)
		},
	)
}

// completeInfraProvision waits for the cluster to be provisioning,
// completes the provision job, and waits for the cluster to be provisioned.
func completeInfraProvision(t *testing.T, kubeClient *kubefake.Clientset, capiClient capiclientset.Interface, cluster *capiv1.Cluster) bool {
	if !completeClusterProcessingJob(
		t,
		"provision",
		kubeClient,
		capiClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "infra"),
		waitForClusterProvisioned,
	) {
		return false
	}

	storedCluster, err := getCluster(capiClient, cluster.Namespace, cluster.Name)
	if !assert.NoError(t, err, "error getting stored cluster") {
		return false
	}
	if !assert.NotEmpty(t, storedCluster.Finalizers, "cluster should have a finalizer for infra provisioning") {
		return false
	}

	return true
}

func clusterJobRef(t *testing.T, kubeClient *kubefake.Clientset, jobPrefix string) func(*capiv1.Cluster) *corev1.LocalObjectReference {
	return func(cluster *capiv1.Cluster) *corev1.LocalObjectReference {
		list, err := kubeClient.BatchV1().Jobs(cluster.Namespace).List(metav1.ListOptions{})
		if err != nil {
			t.Errorf("error retrieving jobs")
			return nil
		}
		for _, job := range list.Items {
			if !metav1.IsControlledBy(&job, cluster) {
				continue
			}
			if strings.HasPrefix(job.Name, jobPrefix) {
				return &corev1.LocalObjectReference{
					Name: job.Name,
				}
			}
		}
		return nil
	}
}

// completeInfraDeprovision waits for the cluster to be deprovisioning,
// completes the deprovision job, and waits for the cluster to have its
// provision finalizer removed.
func completeInfraDeprovision(t *testing.T, kubeClient *kubefake.Clientset, capiClient capiclientset.Interface, cluster *capiv1.Cluster) bool {
	return completeClusterProcessingJob(
		t,
		"deprovision",
		kubeClient,
		capiClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "infra"),
		func(client capiclientset.Interface, namespace, name string) error {
			return waitForObjectToNotExistOrNotHaveFinalizer(
				namespace, name,
				"openshift/cluster-operator-infra",
				func(namespace, name string) (metav1.Object, error) {
					return getCluster(client, namespace, name)
				},
			)
		},
	)
}

// completeControlPlaneInstall waits for the control plane to be
// installing, completes the install job, and waits for the control plane
// to be installed.
func completeControlPlaneInstall(t *testing.T, kubeClient *kubefake.Clientset, capiClient capiclientset.Interface, cluster *capiv1.Cluster) bool {
	return completeClusterProcessingJob(
		t,
		"control plane install",
		kubeClient,
		capiClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "master"),
		func(client capiclientset.Interface, namespace, name string) error {
			return waitForClusterStatus(
				client,
				namespace, name,
				func(cluster *capiv1.Cluster) bool {
					status, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
					if err != nil {
						return false
					}
					return status.ControlPlaneInstalled
				},
			)
		},
	)
}

// completeComponentsInstall waits for the OpenShift components to be
// installing, completes the install job, and waits for the components
// to be installed.
func completeComponentsInstall(t *testing.T, kubeClient *kubefake.Clientset, capiClient capiclientset.Interface, cluster *capiv1.Cluster) bool {
	return completeClusterProcessingJob(
		t,
		"components install",
		kubeClient,
		capiClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "components"),
		func(client capiclientset.Interface, namespace, name string) error {
			return waitForClusterStatus(
				client,
				namespace, name,
				func(cluster *capiv1.Cluster) bool {
					status, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
					if err != nil {
						return false
					}
					return status.ComponentsInstalled
				},
			)
		},
	)
}

// completeNodeConfigInstall waits for the node config to be
// installing, completes the install job, and waits for the node config
// to be installed.
func completeNodeConfigInstall(t *testing.T, kubeClient *kubefake.Clientset, capiClient capiclientset.Interface, cluster *capiv1.Cluster) bool {
	return completeClusterProcessingJob(
		t,
		"node config install",
		kubeClient,
		capiClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "nodeconfig"),
		func(client capiclientset.Interface, namespace, name string) error {
			return waitForClusterStatus(
				client,
				namespace, name,
				func(cluster *capiv1.Cluster) bool {
					status, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
					if err != nil {
						return false
					}
					return status.NodeConfigInstalled
				},
			)
		},
	)
}

// completeDeployClusterAPIInstall waits for the cluster-api-deploy to be
// installing, completes the install job, and waits for the cluster-api-deploy
// to be installed.
func completeDeployClusterAPIInstall(t *testing.T, kubeClient *kubefake.Clientset, capiClient capiclientset.Interface, cluster *capiv1.Cluster) bool {
	return completeClusterProcessingJob(
		t,
		"deploy cluster api install",
		kubeClient,
		capiClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "deployclusterapi"),
		func(client capiclientset.Interface, namespace, name string) error {
			return waitForClusterStatus(
				client,
				namespace, name,
				func(cluster *capiv1.Cluster) bool {
					status, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
					if err != nil {
						return false
					}
					return status.ClusterAPIInstalled
				},
			)
		},
	)
}

func completeJob(kubeClient *kubefake.Clientset, job *kbatch.Job) error {
	job.Status.Conditions = []kbatch.JobCondition{
		{
			Type:   kbatch.JobComplete,
			Status: kapi.ConditionTrue,
		},
	}
	_, err := kubeClient.Batch().Jobs(job.Namespace).Update(job)
	return err
}

func isJobComplete(job *kbatch.Job) bool {
	if job.Status.Conditions == nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == kbatch.JobComplete &&
			condition.Status == kapi.ConditionTrue {
			return true
		}
	}
	return false
}

func getCluster(capiClient capiclientset.Interface, namespace, name string) (*capiv1.Cluster, error) {
	return capiClient.ClusterV1alpha1().Clusters(namespace).Get(name, metav1.GetOptions{})
}

func getMachineSet(capiClient capiclientset.Interface, namespace, name string) (*capiv1.MachineSet, error) {
	return capiClient.ClusterV1alpha1().MachineSets(namespace).Get(name, metav1.GetOptions{})
}

func getJob(kubeClient *kubefake.Clientset, namespace, name string) (*kbatch.Job, error) {
	return kubeClient.Batch().Jobs(namespace).Get(name, metav1.GetOptions{})
}
