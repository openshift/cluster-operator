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
	batchv1 "k8s.io/api/batch/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"

	"github.com/openshift/cluster-operator/pkg/ansible"
	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	"github.com/openshift/cluster-operator/pkg/controller"
	clusterinstallcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterinstall"
	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
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
	clustopClient clustopclientset.Interface,
	capiClient capiclientset.Interface,
) *clusterinstallcontroller.Controller {
	return clusterinstallcontroller.NewController(
		controllerName,
		&installStrategy{},
		playbooks,
		clusterInformer,
		machineSetInformer,
		nil,
		jobInformer,
		kubeClient,
		clustopClient,
		capiClient,
	)
}

type installStrategy struct{}

var _ clusterinstallcontroller.InstallJobDecorationStrategy = (*installStrategy)(nil)

func (s *installStrategy) ReadyToInstall(cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet) bool {
	if !cluster.ClusterDeploymentStatus.ControlPlaneInstalled {
		return false
	}
	return cluster.ClusterDeploymentStatus.ComponentsInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterDeploymentStatus.ComponentsInstalledJobMachineSetGeneration != masterMachineSet.Generation
}

func (s *installStrategy) DecorateJobGeneratorExecutor(executor *ansible.JobGeneratorExecutor, cluster *clustop.CombinedCluster) error {
	infraSize, err := controller.GetInfraSize(cluster)
	if err != nil {
		return err
	}
	executor.WithInfraSize(infraSize)
	return nil
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet, job *batchv1.Job) {
	cluster.ClusterDeploymentStatus.ComponentsInstalled = succeeded
	cluster.ClusterDeploymentStatus.ComponentsInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterDeploymentStatus.ComponentsInstalledJobMachineSetGeneration = masterMachineSet.Generation
}

func (s *installStrategy) ConvertJobSyncConditionType(conditionType controller.JobSyncConditionType) clustop.ClusterConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clustop.ComponentsInstalling
	case controller.JobSyncProcessed:
		return clustop.ComponentsInstalled
	case controller.JobSyncProcessingFailed:
		return clustop.ComponentsInstallationFailed
	default:
		return clustop.ClusterConditionType("")
	}
}
