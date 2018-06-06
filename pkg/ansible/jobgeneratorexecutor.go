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

package ansible

import (
	"fmt"
	"strings"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"

	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const machineKeysAnnotation = "clusteroperator.openshift.io/machines"

// JobGeneratorExecutor is used to execute a JobGenerator to create a job for
// a cluster.
type JobGeneratorExecutor struct {
	jobGenerator        JobGenerator
	playbooks           []string
	cluster             *clustop.CombinedCluster
	clusterVersion      *clustop.ClusterVersion
	forCluster          bool
	forMasterMachineSet bool
	infraSize           *int
	serviceAccount      *kapi.ServiceAccount
	masterMachines      []*capi.Machine
}

// NewJobGeneratorExecutorForCluster creates a JobGeneratorExecutor
// that creates a job for the cluster.
func NewJobGeneratorExecutorForCluster(jobGenerator JobGenerator, playbooks []string, cluster *clustop.CombinedCluster, clusterVersion *clustop.ClusterVersion, infraSize int) *JobGeneratorExecutor {
	return &JobGeneratorExecutor{
		jobGenerator:   jobGenerator,
		playbooks:      playbooks,
		cluster:        cluster,
		clusterVersion: clusterVersion,
		forCluster:     true,
		infraSize:      &infraSize,
	}
}

// NewJobGeneratorExecutorForMasterMachineSet creates a JobGeneratorExecutor
// that creates a job for the master machine set of a cluster.
func NewJobGeneratorExecutorForMasterMachineSet(jobGenerator JobGenerator, playbooks []string, cluster *clustop.CombinedCluster, clusterVersion *clustop.ClusterVersion) *JobGeneratorExecutor {
	return &JobGeneratorExecutor{
		jobGenerator:        jobGenerator,
		playbooks:           playbooks,
		cluster:             cluster,
		clusterVersion:      clusterVersion,
		forMasterMachineSet: true,
	}
}

// NewJobGeneratorExecutorForComputeMachineSet creates a JobGeneratorExecutor
// that creates a job for the compute machine sets of a cluster.
func NewJobGeneratorExecutorForComputeMachineSet(jobGenerator JobGenerator, playbooks []string, cluster *clustop.CombinedCluster, clusterVersion *clustop.ClusterVersion) *JobGeneratorExecutor {
	return &JobGeneratorExecutor{
		jobGenerator:        jobGenerator,
		playbooks:           playbooks,
		cluster:             cluster,
		clusterVersion:      clusterVersion,
		forMasterMachineSet: false,
	}
}

// Execute runs the JobGenerator to create the job.
func (e *JobGeneratorExecutor) Execute(name string) (*kbatch.Job, *kapi.ConfigMap, error) {
	var (
		vars string
		err  error
	)
	switch {
	case e.forCluster:
		vars, err = GenerateClusterWideVars(
			e.cluster.ClusterDeploymentSpec.ClusterID,
			&e.cluster.ClusterDeploymentSpec.Hardware,
			e.clusterVersion,
			*e.infraSize,
		)
	case e.infraSize == nil:
		vars, err = GenerateClusterWideVarsForMachineSet(
			e.forMasterMachineSet,
			e.cluster.ClusterDeploymentSpec.ClusterID,
			&e.cluster.ClusterDeploymentSpec.Hardware,
			e.clusterVersion,
		)
	default:
		vars, err = GenerateClusterWideVarsForMachineSetWithInfraSize(
			e.forMasterMachineSet,
			e.cluster.ClusterDeploymentSpec.ClusterID,
			&e.cluster.ClusterDeploymentSpec.Hardware,
			e.clusterVersion,
			*e.infraSize,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	image, pullPolicy := GetAnsibleImageForClusterVersion(e.clusterVersion)
	var (
		job       *kbatch.Job
		configMap *kapi.ConfigMap
	)
	inventory := DefaultInventory
	if e.masterMachines != nil {
		inventory, err = GenerateInventoryForMasterMachines(e.masterMachines)
		if err != nil {
			return nil, nil, err
		}
	}

	if e.serviceAccount == nil {
		job, configMap = e.jobGenerator.GeneratePlaybooksJob(
			name,
			&e.cluster.ClusterDeploymentSpec.Hardware,
			e.playbooks,
			inventory,
			vars,
			image,
			pullPolicy,
		)
	} else {
		job, configMap = e.jobGenerator.GeneratePlaybooksJobWithServiceAccount(
			name,
			&e.cluster.ClusterDeploymentSpec.Hardware,
			e.playbooks,
			inventory,
			vars,
			image,
			pullPolicy,
			e.serviceAccount,
		)
	}

	if e.masterMachines != nil {
		SetJobMachineKeys(job, e.masterMachines)
	}
	return job, configMap, nil
}

// WithInfraSize modifies the JobGeneratorExecutor so that the job that it creates
// includes infra size in its ansible variables.
func (e *JobGeneratorExecutor) WithInfraSize(size int) *JobGeneratorExecutor {
	e.infraSize = &size
	return e
}

// WithServiceAccount modifies the JobGeneratorExecutor so that the job that it
// creates is supplied the specified service account.
func (e *JobGeneratorExecutor) WithServiceAccount(serviceAccount *kapi.ServiceAccount) *JobGeneratorExecutor {
	e.serviceAccount = serviceAccount
	return e
}

func (e *JobGeneratorExecutor) WithMasterMachines(machines []*capi.Machine) *JobGeneratorExecutor {
	e.masterMachines = machines
	return e
}

func serializeMachineKeys(machines []*capi.Machine) string {
	keys := []string{}
	for _, m := range machines {
		keys = append(keys, fmt.Sprintf("%s/%s", m.Namespace, m.Name))
	}
	return strings.Join(keys, ",")
}

func SetJobMachineKeys(job *kbatch.Job, machines []*capi.Machine) {
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[machineKeysAnnotation] = serializeMachineKeys(machines)
}

func GetJobMachineKeys(job *kbatch.Job) []string {
	result := []string{}
	value, ok := job.Annotations[machineKeysAnnotation]
	if !ok {
		return result
	}
	return strings.Split(value, ",")
}
