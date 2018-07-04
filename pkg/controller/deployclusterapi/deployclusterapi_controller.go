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

package deployclusterapi

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"

	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	"github.com/openshift/cluster-operator/pkg/controller"
	clusterinstallcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterinstall"
	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
)

const (
	// TODO: move to config map for the controllers once we have one
	// Re-execute the job every 2-4 hours.
	configManagementInterval = 2 * time.Hour

	controllerName = "deployclusterapi"

	playbook = "playbooks/cluster-api-prep/deploy-cluster-api.yaml"
)

// NewController returns a new *Controller for cluster-api resources.
func NewController(
	clusterInformer capiinformers.ClusterInformer,
	machineSetInformer capiinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clustopclientset.Interface,
	capiClient capiclientset.Interface,
) *clusterinstallcontroller.Controller {
	return clusterinstallcontroller.NewController(
		controllerName,
		&installStrategy{},
		[]string{playbook},
		clusterInformer,
		machineSetInformer,
		jobInformer,
		kubeClient,
		clustopClient,
		capiClient,
	)
}

type installStrategy struct{}

func (s *installStrategy) ReadyToInstall(cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet) bool {
	if !cluster.ClusterProviderStatus.ControlPlaneInstalled {
		return false
	}
	return cluster.ClusterProviderStatus.ClusterAPIInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterProviderStatus.ClusterAPIInstalledJobMachineSetGeneration != masterMachineSet.GetGeneration()
}

func (s *installStrategy) IncludeInfraSizeInAnsibleVars() bool {
	return false
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet, job *batchv1.Job) {
	cluster.ClusterProviderStatus.ClusterAPIInstalled = succeeded
	cluster.ClusterProviderStatus.ClusterAPIInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterProviderStatus.ClusterAPIInstalledJobMachineSetGeneration = masterMachineSet.GetGeneration()
	cluster.ClusterProviderStatus.ClusterAPIInstalledTime = job.Status.CompletionTime
}

func (s *installStrategy) ConvertJobSyncConditionType(conditionType controller.JobSyncConditionType) clustop.ClusterConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clustop.ClusterAPIInstalling
	case controller.JobSyncProcessed:
		return clustop.ClusterAPIInstalled
	case controller.JobSyncProcessingFailed:
		return clustop.ClusterAPIInstallationFailed
	default:
		return clustop.ClusterConditionType("")
	}
}

func (s *installStrategy) GetReprocessInterval() time.Duration {
	return configManagementInterval
}

func (s *installStrategy) GetLastJobSuccess(cluster *clustop.CombinedCluster) *time.Time {
	if cluster.ClusterProviderStatus.ClusterAPIInstalledTime == nil {
		return nil
	}
	return &cluster.ClusterProviderStatus.ClusterAPIInstalledTime.Time
}
