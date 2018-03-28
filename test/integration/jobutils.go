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
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	kbatch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	// avoid error `clusteroperator/v1alpha1 is not enabled`
	_ "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/install"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
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
		func(obj metav1.Object) bool { return getJobRef(obj) != nil },
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
	clusterOperatorClient clientset.Interface,
	namespace, name string,
	getJobRef func(*v1alpha1.Cluster) *corev1.LocalObjectReference,
	verifyJobCompleted func(clientset.Interface /*namespace*/, string /*name*/, string) error,
) bool {
	return completeProcessingJob(
		t,
		jobLabel,
		kubeClient,
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			return getCluster(clusterOperatorClient, namespace, name)
		},
		func(obj metav1.Object) *corev1.LocalObjectReference {
			return getJobRef(obj.(*v1alpha1.Cluster))
		},
		func(namespace, name string) error {
			return verifyJobCompleted(clusterOperatorClient, namespace, name)
		},
	)
}

// completeMachineSetProcessingJob completes a processing job for the machine
// set with the specified namespace and name. Verification is performed to
// wait for the machine set to be in a state where the job is running before
// completing the job and to wait for the machine set to be in a state where
// the job has been completed successfully after the job is completed.
func completeMachineSetProcessingJob(
	t *testing.T,
	jobLabel string,
	kubeClient *kubefake.Clientset,
	clusterOperatorClient clientset.Interface,
	namespace, name string,
	getJobRef func(*v1alpha1.MachineSet) *corev1.LocalObjectReference,
	verifyJobCompleted func(clientset.Interface /*namespace*/, string /*name*/, string) error,
) bool {
	return completeProcessingJob(
		t,
		jobLabel,
		kubeClient,
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			return getMachineSet(clusterOperatorClient, namespace, name)
		},
		func(obj metav1.Object) *corev1.LocalObjectReference {
			return getJobRef(obj.(*v1alpha1.MachineSet))
		},
		func(namespace, name string) error {
			return verifyJobCompleted(clusterOperatorClient, namespace, name)
		},
	)
}

// completeMachineSetsProcessingJobs completes the processing jobs for the
// specified machine sets. Verification is performed to
// wait for the machine sets to be in a state where the job is running before
// completing the jobs and to wait for the machine sets to be in a state where
// the job has been completed successfully after the jobs are completed.
func completeMachineSetsProcessingJobs(
	machineSets []*v1alpha1.MachineSet,
	verification func(*v1alpha1.MachineSet) bool,
) bool {
	errCh := make(chan error, len(machineSets))

	var wg sync.WaitGroup
	wg.Add(len(machineSets))
	for i, ms := range machineSets {
		go func(ix int, machineSet *v1alpha1.MachineSet) {
			defer wg.Done()
			if !verification(machineSet) {
				errCh <- fmt.Errorf("verification of machine set %s/%s failed for job", machineSet.Namespace, machineSet.Name)
			}
		}(i, ms)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		if err != nil {
			return false
		}
	default:
	}
	return true
}

// completeInfraProvision waits for the cluster to be provisioning,
// completes the provision job, and waits for the cluster to be provisioned.
func completeInfraProvision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, cluster *v1alpha1.Cluster) bool {
	if !completeClusterProcessingJob(
		t,
		"provision",
		kubeClient,
		clusterOperatorClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "infra"),
		waitForClusterProvisioned,
	) {
		return false
	}

	storedCluster, err := getCluster(clusterOperatorClient, cluster.Namespace, cluster.Name)
	if !assert.NoError(t, err, "error getting stored cluster") {
		return false
	}
	if !assert.NotEmpty(t, storedCluster.Finalizers, "cluster should have a finalizer for infra provisioning") {
		return false
	}

	return true
}

func clusterJobRef(t *testing.T, kubeClient *kubefake.Clientset, jobPrefix string) func(*v1alpha1.Cluster) *corev1.LocalObjectReference {
	return func(cluster *v1alpha1.Cluster) *corev1.LocalObjectReference {
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

func machineSetJobRef(t *testing.T, kubeClient *kubefake.Clientset, jobPrefix string) func(*v1alpha1.MachineSet) *corev1.LocalObjectReference {
	return func(machineSet *v1alpha1.MachineSet) *corev1.LocalObjectReference {
		list, err := kubeClient.BatchV1().Jobs(machineSet.Namespace).List(metav1.ListOptions{})
		if err != nil {
			t.Errorf("error retrieving jobs")
			return nil
		}
		for _, job := range list.Items {
			if !metav1.IsControlledBy(&job, machineSet) {
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
func completeInfraDeprovision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, cluster *v1alpha1.Cluster) bool {
	return completeClusterProcessingJob(
		t,
		"deprovision",
		kubeClient,
		clusterOperatorClient,
		cluster.Namespace, cluster.Name,
		clusterJobRef(t, kubeClient, "infra"),
		func(client clientset.Interface, namespace, name string) error {
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

// completeMachineSetProvision waits for the machine set to be
// provisioning, completes the provision job, and waits for the machine set
// to be provisioned.
func completeMachineSetProvision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	if !completeMachineSetProcessingJob(
		t,
		"provision",
		kubeClient,
		clusterOperatorClient,
		machineSet.Namespace, machineSet.Name,
		machineSetJobRef(t, kubeClient, "provision"),
		func(client clientset.Interface, namespace, name string) error {
			return waitForMachineSetStatus(
				client,
				namespace, name,
				func(machineSet *v1alpha1.MachineSet) bool {
					return machineSet.Status.Provisioned
				},
			)
		},
	) {
		return false
	}

	storedMachineSet, err := getMachineSet(clusterOperatorClient, machineSet.Namespace, machineSet.Name)
	if !assert.NoError(t, err, "error getting stored machine set") {
		return false
	}
	if !assert.NotEmpty(t, storedMachineSet.Finalizers, "machine set should have a finalizer for provisioning") {
		return false
	}

	return true
}

// completeComputeMachineSetsProvision waits for the compute machine sets of
// the cluster to be provisioning, completes the provision jobs, and waits for
// the machine set to be provisioned.
func completeComputeMachineSetsProvision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, cluster *v1alpha1.Cluster) bool {
	machineSets, err := getComputeMachineSets(clusterOperatorClient, cluster)
	if !assert.NoError(t, err, "could not get machine sets for cluster") {
		return false
	}
	return completeMachineSetsProcessingJobs(
		machineSets,
		func(machineSet *v1alpha1.MachineSet) bool {
			return completeMachineSetProvision(t, kubeClient, clusterOperatorClient, machineSet)
		},
	)
}

// completeInfraDeprovisionJob waits for the cluster to be deprovisioning,
// completes the deprovision job, and waits for the cluster to have its
// provision finalizer removed.
func completeMachineSetDeprovision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	return completeMachineSetProcessingJob(
		t,
		"deprovision",
		kubeClient,
		clusterOperatorClient,
		machineSet.Namespace, machineSet.Name,
		machineSetJobRef(t, kubeClient, "provision"),
		func(client clientset.Interface, namespace, name string) error {
			return waitForObjectToNotExistOrNotHaveFinalizer(
				namespace, name,
				"openshift/cluster-operator-provision",
				func(namespace, name string) (metav1.Object, error) {
					return getCluster(client, namespace, name)
				},
			)
		},
	)
}

// completeComputeMachineSetsProvision waits for the compute machine sets of
// the cluster to be deprovisioning, completes the deprovision jobs, and waits
// for the machine sets to have their provision finalizers removed.
func completeMachineSetsDeprovision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, cluster *v1alpha1.Cluster) bool {
	machineSets, err := getMachineSetsForCluster(clusterOperatorClient, cluster)
	if !assert.NoError(t, err, "could not get machine sets for cluster") {
		return false
	}
	return completeMachineSetsProcessingJobs(
		machineSets,
		func(machineSet *v1alpha1.MachineSet) bool {
			return completeMachineSetDeprovision(t, kubeClient, clusterOperatorClient, machineSet)
		},
	)
}

// completeMachineSetInstall waits for the machine set to be
// installing, completes the install job, and waits for the machine set
// to be installed.
func completeMachineSetInstall(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	return completeMachineSetProcessingJob(
		t,
		"install",
		kubeClient,
		clusterOperatorClient,
		machineSet.Namespace, machineSet.Name,
		machineSetJobRef(t, kubeClient, "master"),
		func(client clientset.Interface, namespace, name string) error {
			return waitForMachineSetStatus(
				client,
				namespace, name,
				func(machineSet *v1alpha1.MachineSet) bool {
					return machineSet.Status.Installed
				},
			)
		},
	)
}

// completeMachineSetProvision waits for the machine set to be
// accepting, completes the accept job, and waits for the machine set
// to be accepted.
func completeMachineSetAccept(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	return completeMachineSetProcessingJob(
		t,
		"accept",
		kubeClient,
		clusterOperatorClient,
		machineSet.Namespace, machineSet.Name,
		machineSetJobRef(t, kubeClient, "accept"),
		func(client clientset.Interface, namespace, name string) error {
			return waitForMachineSetStatus(
				client,
				namespace, name,
				func(machineSet *v1alpha1.MachineSet) bool {
					return machineSet.Status.Accepted
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
