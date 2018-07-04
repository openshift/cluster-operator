/*
Copyright 2017 The Kubernetes Authors.

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

package master

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	batchv1 "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	v1rbac "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	controllerName = "master"
	playbook       = "playbooks/cluster-operator/aws/install_masters.yml"

	serviceAccountName = "cluster-installer"
	roleBindingName    = "cluster-installer"

	// this name comes from the created ClusterRole in the deployment
	// template contrib/examples/deploy.yaml
	secretCreatorRoleName = "clusteroperator.openshift.io:master-controller"
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
	installStrategy := &installStrategy{
		kubeClient: kubeClient,
	}
	controller := clusterinstallcontroller.NewController(
		controllerName,
		installStrategy,
		[]string{playbook},
		clusterInformer,
		machineSetInformer,
		jobInformer,
		kubeClient,
		clustopClient,
		capiClient,
	)
	installStrategy.logger = controller.Logger
	return controller
}

type installStrategy struct {
	kubeClient kubeclientset.Interface
	logger     log.FieldLogger
}

var _ clusterinstallcontroller.InstallJobDecorationStrategy = (*installStrategy)(nil)

func (s *installStrategy) ReadyToInstall(cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet) bool {
	return cluster.ClusterProviderStatus.ControlPlaneInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterProviderStatus.ControlPlaneInstalledJobMachineSetGeneration != masterMachineSet.GetGeneration()
}

func (s *installStrategy) DecorateJobGeneratorExecutor(executor *ansible.JobGeneratorExecutor, cluster *clustop.CombinedCluster) error {
	serviceAccount, err := s.setupServiceAccountForJob(cluster.Namespace)
	if err != nil {
		return fmt.Errorf("error creating or setting up service account %v", err)
	}
	executor.WithServiceAccount(serviceAccount)
	return nil
}

// create a serviceaccount and rolebinding so that the master job
// can save the target cluster's kubeconfig
func (s *installStrategy) setupServiceAccountForJob(clusterNamespace string) (*kapi.ServiceAccount, error) {
	// create new serviceaccount if it doesn't already exist
	currentSA, err := s.kubeClient.Core().ServiceAccounts(clusterNamespace).Get(serviceAccountName, metav1.GetOptions{})
	if err != nil && !apierrs.IsNotFound(err) {
		return nil, fmt.Errorf("error checking for existing serviceaccount")
	}

	if apierrs.IsNotFound(err) {
		currentSA, err = s.kubeClient.Core().ServiceAccounts(clusterNamespace).Create(&kapi.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: serviceAccountName,
			},
		})

		if err != nil {
			return nil, fmt.Errorf("error creating serviceaccount for master job")
		}
	}
	s.logger.Debugf("using serviceaccount: %+v", currentSA)

	// create rolebinding for the serviceaccount
	currentRB, err := s.kubeClient.Rbac().RoleBindings(clusterNamespace).Get(roleBindingName, metav1.GetOptions{})

	if err != nil && !apierrs.IsNotFound(err) {
		return nil, fmt.Errorf("error checking for exising rolebinding")
	}

	if apierrs.IsNotFound(err) {
		rb := &v1rbac.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleBindingName,
				Namespace: clusterNamespace,
			},
			Subjects: []v1rbac.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      currentSA.Name,
					Namespace: clusterNamespace,
				},
			},
			RoleRef: v1rbac.RoleRef{
				Name: secretCreatorRoleName,
				Kind: "ClusterRole",
			},
		}

		currentRB, err = s.kubeClient.Rbac().RoleBindings(clusterNamespace).Create(rb)
		if err != nil {
			return nil, fmt.Errorf("couldn't create rolebinding to clusterrole")
		}
	}
	s.logger.Debugf("rolebinding to serviceaccount: %+v", currentRB)

	return currentSA, nil
}

func (s *installStrategy) IncludeInfraSizeInAnsibleVars() bool {
	return false
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet, job *batchv1.Job) {
	cluster.ClusterProviderStatus.ControlPlaneInstalled = succeeded
	cluster.ClusterProviderStatus.ControlPlaneInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterProviderStatus.ControlPlaneInstalledJobMachineSetGeneration = masterMachineSet.GetGeneration()
}

func (s *installStrategy) ConvertJobSyncConditionType(conditionType controller.JobSyncConditionType) clustop.ClusterConditionType {
	switch conditionType {
	case controller.JobSyncProcessing:
		return clustop.ControlPlaneInstalling
	case controller.JobSyncProcessed:
		return clustop.ControlPlaneInstalled
	case controller.JobSyncProcessingFailed:
		return clustop.ControlPlaneInstallationFailed
	default:
		return clustop.ClusterConditionType("")
	}
}
