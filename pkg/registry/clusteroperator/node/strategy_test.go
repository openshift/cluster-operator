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

package node

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nodeWithOldSpec() *clusteroperator.Node {
	return &clusteroperator.Node{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
}

func nodeWithNewSpec() *clusteroperator.Node {
	b := nodeWithOldSpec()
	return b
}

// TestNodeStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestNodeStrategyTrivial(t *testing.T) {
	if nodeRESTStrategies.NamespaceScoped() {
		t.Errorf("node create must not be namespace scoped")
	}
	if nodeRESTStrategies.NamespaceScoped() {
		t.Errorf("node update must not be namespace scoped")
	}
	if nodeRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("node should not allow create on update")
	}
	if nodeRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("node should not allow unconditional update")
	}
}

// TestNodeCreate
func TestNodeCreate(t *testing.T) {
	// Create a node or nodes
	node := &clusteroperator.Node{}

	// Canonicalize the node
	nodeRESTStrategies.PrepareForCreate(nil, node)
}

// TestNodeUpdate tests that generation is incremented correctly when the
// spec of a Node is updated.
func TestNodeUpdate(t *testing.T) {
	older := nodeWithOldSpec()
	newer := nodeWithOldSpec()

	nodeRESTStrategies.PrepareForUpdate(nil, newer, older)
}
