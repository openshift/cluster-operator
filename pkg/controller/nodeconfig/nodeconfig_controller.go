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

package nodeconfig

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	clusterinstallcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterinstall"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
)

const (
	// TODO: move to config map for the controllers once we have one
	// Re-execute the job every 2-4 hours.
	configManagementInterval = 2 * time.Hour

	controllerName = "nodeconfig"

	playbook = "playbooks/cluster-operator/node-config-daemonset.yml"
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

func (s *installStrategy) ReadyToInstall(cluster *cov1.CombinedCluster, masterMachineSet *capiv1.MachineSet) bool {
	if !cluster.ClusterProviderStatus.ControlPlaneInstalled {
		return false
	}
	return cluster.ClusterProviderStatus.NodeConfigInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterProviderStatus.NodeConfigInstalledJobMachineSetGeneration != masterMachineSet.GetGeneration()
}

func (s *installStrategy) IncludeInfraSizeInAnsibleVars() bool {
	return false
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *cov1.CombinedCluster, masterMachineSet *capiv1.MachineSet, job *batchv1.Job) {
	cluster.ClusterProviderStatus.NodeConfigInstalled = succeeded
	cluster.ClusterProviderStatus.NodeConfigInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterProviderStatus.NodeConfigInstalledJobMachineSetGeneration = masterMachineSet.GetGeneration()
	cluster.ClusterProviderStatus.NodeConfigInstalledTime = job.Status.CompletionTime
}

func (s *installStrategy) ConvertJobSyncConditionType(conditionType cocontroller.JobSyncConditionType) cov1.ClusterConditionType {
	switch conditionType {
	case cocontroller.JobSyncProcessing:
		return cov1.NodeConfigInstalling
	case cocontroller.JobSyncProcessed:
		return cov1.NodeConfigInstalled
	case cocontroller.JobSyncProcessingFailed:
		return cov1.NodeConfigInstallationFailed
	default:
		return cov1.ClusterConditionType("")
	}
}

func (s *installStrategy) GetReprocessInterval() time.Duration {
	return configManagementInterval
}

func (s *installStrategy) GetLastJobSuccess(cluster *cov1.CombinedCluster) *time.Time {
	if cluster.ClusterProviderStatus.NodeConfigInstalledTime == nil {
		return nil
	}
	return &cluster.ClusterProviderStatus.NodeConfigInstalledTime.Time
}
