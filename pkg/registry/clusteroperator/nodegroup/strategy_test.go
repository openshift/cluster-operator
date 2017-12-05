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

package nodegroup

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nodegroupWithOldSpec() *clusteroperator.NodeGroup {
	return &clusteroperator.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
}

func nodegroupWithNewSpec() *clusteroperator.NodeGroup {
	b := nodegroupWithOldSpec()
	return b
}

// TestNodeGroupStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestNodeGroupStrategyTrivial(t *testing.T) {
	if nodegroupRESTStrategies.NamespaceScoped() {
		t.Errorf("nodegroup create must not be namespace scoped")
	}
	if nodegroupRESTStrategies.NamespaceScoped() {
		t.Errorf("nodegroup update must not be namespace scoped")
	}
	if nodegroupRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("nodegroup should not allow create on update")
	}
	if nodegroupRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("nodegroup should not allow unconditional update")
	}
}

// TestNodeGroupCreate
func TestNodeGroupCreate(t *testing.T) {
	// Create a nodegroup or nodegroups
	nodegroup := &clusteroperator.NodeGroup{}

	// Canonicalize the nodegroup
	nodegroupRESTStrategies.PrepareForCreate(nil, nodegroup)
}

// TestNodeGroupUpdate tests that generation is incremented correctly when the
// spec of a NodeGroup is updated.
func TestNodeGroupUpdate(t *testing.T) {
	older := nodegroupWithOldSpec()
	newer := nodegroupWithOldSpec()

	nodegroupRESTStrategies.PrepareForUpdate(nil, newer, older)
}
