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

package components

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"

	coansible "github.com/openshift/cluster-operator/pkg/ansible"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	clusterinstallcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterinstall"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
)

const (
	controllerName = "components"
)

var (
	playbooks = []string{
		"playbooks/cluster-operator/aws/components/openshift-glusterfs.yml",
		"playbooks/cluster-operator/aws/components/openshift-hosted.yml",
		"playbooks/cluster-operator/aws/components/openshift-logging.yml",
		"playbooks/cluster-operator/aws/components/openshift-management.yml",
		"playbooks/cluster-operator/aws/components/openshift-metrics.yml",
		"playbooks/cluster-operator/aws/components/openshift-prometheus.yml",
		"playbooks/cluster-operator/aws/components/openshift-service-catalog.yml",
		"playbooks/cluster-operator/aws/components/openshift-web-console.yml",
	}
)

// NewController returns a new *Controller for cluster-api resources.
func NewController(
	clusterInformer capiinformers.ClusterInformer,
	machineSetInformer capiinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient coclientset.Interface,
	capiClient capiclientset.Interface,
) *clusterinstallcontroller.Controller {

	return clusterinstallcontroller.NewController(
		controllerName,
		&installStrategy{
			kubeClient: kubeClient,
		},
		playbooks,
		clusterInformer,
		machineSetInformer,
		jobInformer,
		kubeClient,
		clustopClient,
		capiClient,
	)
}

type installStrategy struct {
	kubeClient kubeclientset.Interface
}

var _ clusterinstallcontroller.InstallJobDecorationStrategy = (*installStrategy)(nil)

func (s *installStrategy) ReadyToInstall(cluster *cov1.CombinedCluster, masterMachineSet *capiv1.MachineSet) bool {
	if !cluster.ClusterProviderStatus.ControlPlaneInstalled {
		return false
	}
	return cluster.ClusterProviderStatus.ComponentsInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterProviderStatus.ComponentsInstalledJobMachineSetGeneration != masterMachineSet.Generation
}

func (s *installStrategy) DecorateJobGeneratorExecutor(executor *coansible.JobGeneratorExecutor, cluster *cov1.CombinedCluster) error {
	infraSize, err := cocontroller.GetInfraSize(cluster)
	if err != nil {
		return err
	}
	executor.WithInfraSize(infraSize)

	// wait for registryinfra status so we know whether to expect a Secret with
	// S3 bucket creds
	if !cluster.ClusterProviderStatus.RegistryInfraCompleted == true {
		return fmt.Errorf("need to wait for registryinfra controller to be done processing")
	}

	registrySecretFound := true
	registryCredsSecretName := cocontroller.RegistryCredsSecretName(cluster.Name)
	rSecret, err := s.kubeClient.CoreV1().Secrets(cluster.Namespace).Get(registryCredsSecretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		registrySecretFound = false
	} else if err != nil {
		return fmt.Errorf("error while checking for existing registry creds: %v", err)
	}

	accessKey := ""
	secretKey := ""
	if registrySecretFound {
		accessKey = string(rSecret.Data["awsAccessKeyId"])
		secretKey = string(rSecret.Data["awsSecretAccessKey"])
	}

	executor.WithRegistryStorageCreds(accessKey, secretKey)

	return nil
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *cov1.CombinedCluster, masterMachineSet *capiv1.MachineSet, job *batchv1.Job) {
	cluster.ClusterProviderStatus.ComponentsInstalled = succeeded
	cluster.ClusterProviderStatus.ComponentsInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterProviderStatus.ComponentsInstalledJobMachineSetGeneration = masterMachineSet.Generation
}

func (s *installStrategy) ConvertJobSyncConditionType(conditionType cocontroller.JobSyncConditionType) cov1.ClusterConditionType {
	switch conditionType {
	case cocontroller.JobSyncProcessing:
		return cov1.ComponentsInstalling
	case cocontroller.JobSyncProcessed:
		return cov1.ComponentsInstalled
	case cocontroller.JobSyncProcessingFailed:
		return cov1.ComponentsInstallationFailed
	default:
		return cov1.ClusterConditionType("")
	}
}
