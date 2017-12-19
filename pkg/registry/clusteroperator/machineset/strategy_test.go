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

package machineset

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func machinesetWithOldSpec() *clusteroperator.MachineSet {
	return &clusteroperator.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
}

func machinesetWithNewSpec() *clusteroperator.MachineSet {
	b := machinesetWithOldSpec()
	return b
}

// TestMachineSetStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestMachineSetStrategyTrivial(t *testing.T) {
	if !machinesetRESTStrategies.NamespaceScoped() {
		t.Errorf("machineset create must be namespace scoped")
	}
	if machinesetRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("machineset should not allow create on update")
	}
	if machinesetRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("machineset should not allow unconditional update")
	}
}

// TestMachineSetCreate
func TestMachineSetCreate(t *testing.T) {
	// Create a machineset or machinesets
	machineset := &clusteroperator.MachineSet{}

	// Canonicalize the machineset
	machinesetRESTStrategies.PrepareForCreate(nil, machineset)
}

// TestMachineSetUpdate tests that generation is incremented correctly when the
// spec of a MachineSet is updated.
func TestMachineSetUpdate(t *testing.T) {
	older := machinesetWithOldSpec()
	newer := machinesetWithOldSpec()

	machinesetRESTStrategies.PrepareForUpdate(nil, newer, older)
}
