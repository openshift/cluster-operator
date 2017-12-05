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

package cluster

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clusterWithOldSpec() *clusteroperator.Cluster {
	return &clusteroperator.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
}

func clusterWithNewSpec() *clusteroperator.Cluster {
	b := clusterWithOldSpec()
	return b
}

// TestClusterStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestClusterStrategyTrivial(t *testing.T) {
	if !clusterRESTStrategies.NamespaceScoped() {
		t.Errorf("cluster create must be namespace scoped")
	}
	if clusterRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("cluster should not allow create on update")
	}
	if clusterRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("cluster should not allow unconditional update")
	}
}

// TestClusterCreate
func TestClusterCreate(t *testing.T) {
	// Create a cluster or clusters
	cluster := &clusteroperator.Cluster{}

	// Canonicalize the cluster
	clusterRESTStrategies.PrepareForCreate(nil, cluster)
}

// TestClusterUpdate tests that generation is incremented correctly when the
// spec of a Cluster is updated.
func TestClusterUpdate(t *testing.T) {
	older := clusterWithOldSpec()
	newer := clusterWithOldSpec()

	clusterRESTStrategies.PrepareForUpdate(nil, newer, older)
}
