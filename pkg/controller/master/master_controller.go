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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/errors"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	capilisters "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"

	"github.com/openshift/cluster-operator/pkg/ansible"
	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	"github.com/openshift/cluster-operator/pkg/controller"
	clusterinstallcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterinstall"
	"github.com/openshift/cluster-operator/pkg/logging"
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
	machineInformer capiinformers.MachineInformer,
	jobInformer batchinformers.JobInformer,
	kubeClient kubeclientset.Interface,
	clustopClient clustopclientset.Interface,
	capiClient capiclientset.Interface,
) *clusterinstallcontroller.Controller {
	installStrategy := &installStrategy{
		kubeClient:       kubeClient,
		capiClient:       capiClient,
		machineLister:    machineInformer.Lister(),
		machineSetLister: machineSetInformer.Lister(),
	}
	controller := clusterinstallcontroller.NewController(
		controllerName,
		installStrategy,
		[]string{playbook},
		clusterInformer,
		machineSetInformer,
		machineInformer,
		jobInformer,
		kubeClient,
		clustopClient,
		capiClient,
	)
	installStrategy.logger = controller.Logger
	return controller
}

type installStrategy struct {
	kubeClient       kubeclientset.Interface
	capiClient       capiclientset.Interface
	logger           log.FieldLogger
	machineLister    capilisters.MachineLister
	machineSetLister capilisters.MachineSetLister
}

var _ clusterinstallcontroller.InstallJobDecorationStrategy = (*installStrategy)(nil)

func (s *installStrategy) ReadyToInstall(cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet) bool {
	return s.machinesReadyToInstall(masterMachineSet) && (cluster.ClusterDeploymentStatus.ControlPlaneInstalledJobClusterGeneration != cluster.Generation ||
		cluster.ClusterDeploymentStatus.ControlPlaneInstalledJobMachineSetGeneration != masterMachineSet.GetGeneration())
}

func (s *installStrategy) DecorateJobGeneratorExecutor(executor *ansible.JobGeneratorExecutor, cluster *clustop.CombinedCluster) error {
	serviceAccount, err := s.setupServiceAccountForJob(cluster.Namespace)
	if err != nil {
		return fmt.Errorf("error creating or setting up service account %v", err)
	}
	executor.WithServiceAccount(serviceAccount)

	machineSet, err := s.masterMachineSet(cluster)
	if err != nil {
		return fmt.Errorf("error fetching master machineset for cluster %s/%s: %v", cluster.Namespace, cluster.Name, err)
	}
	machines, err := s.availableMasterMachines(machineSet)
	if err != nil {
		return fmt.Errorf("error fetching machines for machineset %s/%s: %v", machineSet.Namespace, machineSet.Name, err)
	}
	executor.WithMasterMachines(machines)

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

func (s *installStrategy) masterMachineSet(cluster *clustop.CombinedCluster) (*capi.MachineSet, error) {
	masterMachineSetName := controller.MasterMachineSetName(cluster.Name)
	return s.machineSetLister.MachineSets(cluster.Namespace).Get(masterMachineSetName)
}

func (s *installStrategy) availableMasterMachines(masterMachineSet *capi.MachineSet) ([]*capi.Machine, error) {
	machines := []*capi.Machine{}
	allMachines, err := s.machineLister.Machines(masterMachineSet.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, machine := range allMachines {
		if !metav1.IsControlledBy(machine, masterMachineSet) ||
			machine.DeletionTimestamp != nil {
			continue
		}
		hasPublicName, err := hasPublicDNS(machine)
		if err != nil {
			return nil, fmt.Errorf("error checking whether machine %s/%s has public DNS: %v", machine.Namespace, machine.Name, err)
		}
		if !hasPublicName {
			continue
		}
		machines = append(machines, machine)
	}
	return machines, nil
}

func (s *installStrategy) machinesReadyToInstall(masterMachineSet *capi.MachineSet) bool {
	logger := logging.WithMachineSet(s.logger, masterMachineSet)
	machines, err := s.availableMasterMachines(masterMachineSet)
	if err != nil {
		logger.Errorf("error determining available master machines: %v", err)
		return false
	}
	if masterMachineSet.Spec.Replicas == nil {
		logger.Errorf("no replicas defined for master machineset")
		return false
	}
	// Only start an install if we have the number of replicas specified in the machineset
	if len(machines) < int(*masterMachineSet.Spec.Replicas) {
		return false
	}
	// Of the available machines, determine if any has not had its control plane installed
	// If all of them have a control plane, then no need to run this job.
	// TODO: determine how to handle upgrades, if they require a job.
	for _, m := range machines {
		if m.Status.Versions == nil || len(m.Status.Versions.ControlPlane) == 0 {
			return true
		}
	}
	return false
}

func hasPublicDNS(machine *capi.Machine) (bool, error) {
	providerStatus, err := controller.AWSMachineProviderStatusFromClusterAPIMachine(machine)
	if err != nil {
		return false, err
	}
	return providerStatus.PublicDNS != nil && len(*providerStatus.PublicDNS) > 0, nil
}

func (s *installStrategy) IncludeInfraSizeInAnsibleVars() bool {
	return false
}

func (s *installStrategy) OnInstall(succeeded bool, cluster *clustop.CombinedCluster, masterMachineSet *capi.MachineSet, job *batchv1.Job) {
	if succeeded {
		machineKeys := ansible.GetJobMachineKeys(job)
		err := s.setMachineControlPlaneVersion(machineKeys)
		if err != nil {
			logging.WithMachineSet(s.logger, masterMachineSet).Errorf("error updating machine installed control plane version: %v", err)
			return
		}
	}
	cluster.ClusterDeploymentStatus.ControlPlaneInstalled = succeeded
	cluster.ClusterDeploymentStatus.ControlPlaneInstalledJobClusterGeneration = cluster.Generation
	cluster.ClusterDeploymentStatus.ControlPlaneInstalledJobMachineSetGeneration = masterMachineSet.GetGeneration()
}

func (s *installStrategy) setMachineControlPlaneVersion(machineKeys []string) error {
	errs := []error{}
	for _, machineKey := range machineKeys {
		namespace, name, err := cache.SplitMetaNamespaceKey(machineKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("error splitting machine key (%s): %v", machineKey, err))
		}
		machine, err := s.machineLister.Machines(namespace).Get(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("error retrieving machine %s to update the control plane version", machineKey))
		}
		updatedMachine := machine.DeepCopy()
		if updatedMachine.Status.Versions == nil {
			updatedMachine.Status.Versions = &capi.MachineVersionInfo{}
		}
		updatedMachine.Status.Versions.ControlPlane = machine.Spec.Versions.ControlPlane
		updatedMachine.Status.LastUpdated = metav1.Now()
		_, err = s.capiClient.ClusterV1alpha1().Machines(machine.Namespace).UpdateStatus(updatedMachine)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to update status of machine %s: %v", machineKey, err))
		}
	}
	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
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
