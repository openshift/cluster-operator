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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-operator/pkg/ansible"
	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
)

// InstallStrategy is the strategy that a controller installing in a
// cluster will use.
// Implement JobSyncReprocessStrategy if successful installation jobs should be
// reprocessed at regular intervals.
type InstallStrategy interface {
	ReadyToInstall(cluster *clustop.CombinedCluster, masterMachineSet metav1.Object) bool

	OnInstall(succeeded bool, cluster *clustop.CombinedCluster, masterMachineSet metav1.Object, job *batchv1.Job)

	ConvertJobSyncConditionType(conditionType controller.JobSyncConditionType) clustop.ClusterConditionType
}

// InstallJobDecorationStrategy is an interface that can be added to
// implementations of InstallStrategy for controllers that need to decorate the
// JobGeneratorExecutor.
type InstallJobDecorationStrategy interface {
	// DecorateJobGeneratorExecutor decorates the executor that will be used
	// to create the job for the specified cluster.
	DecorateJobGeneratorExecutor(executor *ansible.JobGeneratorExecutor, cluster *clustop.CombinedCluster) error
}
