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
	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"

	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

// JobGeneratorExecutor is used to execute a JobGenerator to create a job for
// a cluster.
type JobGeneratorExecutor struct {
	jobGenerator        JobGenerator
	playbooks           []string
	cluster             *clustop.CombinedCluster
	clusterVersion      *clustop.ClusterVersion
	forMasterMachineSet bool
	infraSize           *int
	serviceAccount      *kapi.ServiceAccount
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
	if e.infraSize == nil {
		vars, err = GenerateClusterWideVarsForMachineSet(e.forMasterMachineSet, e.cluster.Name, &e.cluster.ClusterOperatorSpec.Hardware, e.clusterVersion)
	} else {
		vars, err = GenerateClusterWideVarsForMachineSetWithInfraSize(e.forMasterMachineSet, e.cluster.Name, &e.cluster.ClusterOperatorSpec.Hardware, e.clusterVersion, *e.infraSize)
	}
	if err != nil {
		return nil, nil, err
	}
	image, pullPolicy := GetAnsibleImageForClusterVersion(e.clusterVersion)
	var (
		job       *kbatch.Job
		configMap *kapi.ConfigMap
	)
	if e.serviceAccount == nil {
		job, configMap = e.jobGenerator.GeneratePlaybooksJob(
			name,
			&e.cluster.ClusterOperatorSpec.Hardware,
			e.playbooks,
			DefaultInventory,
			vars,
			image,
			pullPolicy,
		)
	} else {
		job, configMap = e.jobGenerator.GeneratePlaybooksJobWithServiceAccount(
			name,
			&e.cluster.ClusterOperatorSpec.Hardware,
			e.playbooks,
			DefaultInventory,
			vars,
			image,
			pullPolicy,
			e.serviceAccount,
		)
	}
	return job, configMap, nil
}

// WithInfraSize modifies the JobGeneratorExecutor so that the job that it creates
// includes infra size in its ansible variables.
func (e *JobGeneratorExecutor) WithInfraSize(cluster *clustop.CombinedCluster) *JobGeneratorExecutor {
	infraSize, _ := getInfraSize(cluster.ClusterOperatorSpec)
	e.infraSize = &infraSize
	return e
}

// WithServiceAccount modifies the JobGeneratorExecutor so that the job that it
// creates is supplied the specified service account.
func (e *JobGeneratorExecutor) WithServiceAccount(serviceAccount *kapi.ServiceAccount) *JobGeneratorExecutor {
	e.serviceAccount = serviceAccount
	return e
}
