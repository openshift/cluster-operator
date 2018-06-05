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

package fake

import (
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"

	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	clustoplog "github.com/openshift/cluster-operator/pkg/logging"
)

// Actuator is a fake actuator
type Actuator struct {
	logger   *log.Entry
	machines sync.Map
}

// NewActuator returns a new fake Actuator
func NewActuator(logger *log.Entry) *Actuator {
	actuator := &Actuator{
		logger: logger,
	}
	return actuator
}

// Create logs a create call
func (a *Actuator) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	clustoplog.WithMachine(clustoplog.WithCluster(a.logger, cluster), machine).Infof("creating machine %#v", machine)
	a.machines.LoadOrStore(machineKey(machine), true)
	return nil
}

// Delete logs a delete call
func (a *Actuator) Delete(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	clustoplog.WithMachine(clustoplog.WithCluster(a.logger, cluster), machine).Infof("deleting machine %#v", machine)
	a.machines.Delete(machineKey(machine))
	return nil
}

// Update logs an update call
func (a *Actuator) Update(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	clustoplog.WithMachine(clustoplog.WithCluster(a.logger, cluster), machine).Infof("updating machine %#v", machine)
	if _, ok := a.machines.Load(machineKey(machine)); !ok {
		return fmt.Errorf("machine not found")
	}
	return nil
}

// Exists logs the exists call and returns true
func (a *Actuator) Exists(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (bool, error) {
	clustoplog.WithMachine(clustoplog.WithCluster(a.logger, cluster), machine).Infof("checking if machine exists")
	_, ok := a.machines.Load(machineKey(machine))
	return ok, nil
}

func machineKey(machine *clusterv1.Machine) string {
	return fmt.Sprintf("%s/%s", machine.Namespace, machine.Name)
}
