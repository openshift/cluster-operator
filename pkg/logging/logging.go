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

package logging

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	log "github.com/sirupsen/logrus"
)

// WithMachineSet expands a logger's context to include info about the given machineset.
func WithMachineSet(logger log.FieldLogger, machineSet *clusteroperator.MachineSet) log.FieldLogger {
	return WithGenericMachineSet(logger, machineSet)
}

// WithAPIMachineSet expands a logger's context to include info about the given machineset.
func WithAPIMachineSet(logger log.FieldLogger, machineSet *clusterapi.MachineSet) log.FieldLogger {
	return WithGenericMachineSet(logger, machineSet)
}

// WithGenericMachineSet expands a logger's context to include info about the given machineset.
func WithGenericMachineSet(logger log.FieldLogger, machineSet metav1.Object) log.FieldLogger {
	return WithGenericObject(logger, "machineset", machineSet)
}

// WithCluster expands a logger's context to include info about the given cluster.
func WithCluster(logger log.FieldLogger, cluster *clusteroperator.Cluster) log.FieldLogger {
	return WithGenericCluster(logger, cluster)
}

// WithAPICluster expands a logger's context to include info about the given cluster.
func WithAPICluster(logger log.FieldLogger, cluster *clusterapi.Cluster) log.FieldLogger {
	return WithGenericCluster(logger, cluster)
}

// WithGenericCluster expands a logger's context to include info about the given cluster.
func WithGenericCluster(logger log.FieldLogger, cluster metav1.Object) log.FieldLogger {
	return WithGenericObject(logger, "cluster", cluster)
}

// WithGenericObject expands a logger's context to include info about the given object.
func WithGenericObject(logger log.FieldLogger, objectType string, obj metav1.Object) log.FieldLogger {
	return logger.WithField(objectType, fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName()))
}

// WithClusterAPICluster expands a logger's context to include info about the given cluster.
func WithClusterAPICluster(logger log.FieldLogger, cluster *clusterv1.Cluster) log.FieldLogger {
	return logger.WithField("cluster", fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name))
}

// WithClusterAPIMachineSet expands a logger's context to include info about the given machineset.
func WithClusterAPIMachineSet(logger log.FieldLogger, machineSet *clusterv1.MachineSet) log.FieldLogger {
	return logger.WithField("machineset", fmt.Sprintf("%s/%s", machineSet.Namespace, machineSet.Name))
}

// WithClusterAPIMachine expands a logger's context to include info about the given machine.
func WithClusterAPIMachine(logger log.FieldLogger, machine *clusterv1.Machine) log.FieldLogger {
	return logger.WithField("machine", fmt.Sprintf("%s/%s", machine.Namespace, machine.Name))
}
