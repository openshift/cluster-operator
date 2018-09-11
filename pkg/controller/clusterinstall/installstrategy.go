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

package clusterinstall

import (
	batchv1 "k8s.io/api/batch/v1"

	coansible "github.com/openshift/cluster-operator/pkg/ansible"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

// InstallStrategy is the strategy that a controller installing in a
// cluster will use.
// Implement JobSyncReprocessStrategy if successful installation jobs should be
// reprocessed at regular intervals.
type InstallStrategy interface {
	ReadyToInstall(cluster *cov1.CombinedCluster, masterMachineSet *capiv1.MachineSet) bool

	OnInstall(succeeded bool, cluster *cov1.CombinedCluster, masterMachineSet *capiv1.MachineSet, job *batchv1.Job)

	ConvertJobSyncConditionType(conditionType cocontroller.JobSyncConditionType) cov1.ClusterConditionType
}

// InstallJobDecorationStrategy is an interface that can be added to
// implementations of InstallStrategy for controllers that need to decorate the
// JobGeneratorExecutor.
type InstallJobDecorationStrategy interface {
	// DecorateJobGeneratorExecutor decorates the executor that will be used
	// to create the job for the specified cluster.
	DecorateJobGeneratorExecutor(executor *coansible.JobGeneratorExecutor, cluster *cov1.CombinedCluster) error
}
