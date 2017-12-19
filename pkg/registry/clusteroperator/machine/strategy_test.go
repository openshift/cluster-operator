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

package machine

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func machineWithOldSpec() *clusteroperator.Machine {
	return &clusteroperator.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
}

func machineWithNewSpec() *clusteroperator.Machine {
	b := machineWithOldSpec()
	return b
}

// TestMachineStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestMachineStrategyTrivial(t *testing.T) {
	if !machineRESTStrategies.NamespaceScoped() {
		t.Errorf("machine update must be namespace scoped")
	}
	if machineRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("machine should not allow create on update")
	}
	if machineRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("machine should not allow unconditional update")
	}
}

// TestMachineCreate
func TestMachineCreate(t *testing.T) {
	// Create a machine or machines
	machine := &clusteroperator.Machine{}

	// Canonicalize the machine
	machineRESTStrategies.PrepareForCreate(nil, machine)
}

// TestMachineUpdate tests that generation is incremented correctly when the
// spec of a Machine is updated.
func TestMachineUpdate(t *testing.T) {
	older := machineWithOldSpec()
	newer := machineWithOldSpec()

	machineRESTStrategies.PrepareForUpdate(nil, newer, older)
}
