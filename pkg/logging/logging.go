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

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	log "github.com/sirupsen/logrus"
)

// WithMachineSet expands a logger's context to include info about the given machineset.
func WithMachineSet(logger log.FieldLogger, machineSet *clusteroperator.MachineSet) log.FieldLogger {
	return logger.WithField("machineset", fmt.Sprintf("%s/%s", machineSet.Namespace, machineSet.Name))
}

// WithMachineSet expands a logger's context to include info about the given cluster.
func WithCluster(logger log.FieldLogger, cluster *clusteroperator.Cluster) log.FieldLogger {
	return logger.WithField("cluster", fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name))
}
