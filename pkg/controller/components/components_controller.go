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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"

	"github.com/openshift/cluster-operator/pkg/ansible"
	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	clustopinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	clusterinstallcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterinstall"
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

// NewClustopController returns a new *Controller for cluster-operator
// resources.
func NewClustopController(
	clusterInformer clustopinformers.ClusterInformer,
	machineSetInformer clustopinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clustopclientset.Interface,
) *clusterinstallcontroller.Controller {
	return clusterinstallcontroller.NewClustopController(
		controllerName,
		&installStrategy{
			canGetInfraSize: func(*clustop.CombinedCluster) bool { return true },
			getInfraSize: func(cluster *clustop.CombinedCluster) (int, error) {
				return controller.GetClustopInfraSize(cluster)
			},
		},
		playbooks,
		clusterInformer,
		machineSetInformer,
		jobInformer,
		kubeClient,
		clustopClient,
	)
}

// NewCAPIController returns a new *Controller for cluster-api resources.
func NewCAPIController(
	clusterInformer capiinformers.ClusterInformer,
	machineSetInformer capiinformers.MachineSetInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clustopclientset.Interface,
	capiClient capiclientset.Interface,
) *clusterinstallcontroller.Controller {
	machineSetLister := machineSetInformer.Lister()
	return clusterinstallcontroller.NewCAPIController(
		controllerName,
		&installStrategy{
			canGetInfraSize: func(cluster *clustop.CombinedCluster) bool {
				_, err := controller.GetCAPIInfraMachineSet(cluster, machineSetLister)
				return err == nil
			},
			getInfraSize: func(cluster *clustop.CombinedCluster) (int, error) {
				return controller.GetCAPIInfraSize(cluster, machineSetLister)
			},
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
	canGetInfraSize func(*clustop.CombinedCluster) bool
	getInfraSize    func(*clustop.CombinedCluster) (int, error)
}

var _ clusterinstallcontroller.InstallJobDecorationStrategy = (*installStrategy)(nil)

func (s *installStrategy) ReadyToInstall(cluster *clustop.CombinedCluster, masterMachineSet metav1.Object) bool {
	if !cluster.ClusterOperatorStatus.ControlPlaneInstalled {
		return false
	}
	if !s.canGetInfraSize(cluster) {
		return false
	}
	return cluster.ClusterOperatorStatus.ComponentsInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterOperatorStatus.ComponentsInstalledJobMachineSetGeneration != masterMachineSet.GetGeneration()
}

func (s *installStrategy) DecorateJobGeneratorExecutor(executor *ansible.JobGeneratorExecutor, cluster *clustop.CombinedCluster) error {
	infraSize, err := s.getInfraSize(cluster)
	if err != nil {
		return err
	}
	executor.WithInfraSize(infraSize)
	return nil
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *clustop.CombinedCluster, masterMachineSet metav1.Object, job *batchv1.Job) {
	cluster.ClusterOperatorStatus.ComponentsInstalled = succeeded
	cluster.ClusterOperatorStatus.ComponentsInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterOperatorStatus.ComponentsInstalledJobMachineSetGeneration = masterMachineSet.GetGeneration()
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
