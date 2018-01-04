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

package clusterversion

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clusterVersion() *clusteroperator.ClusterVersion {
	return &clusteroperator.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{},
	}
}

// TestClusterVersionStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestClusterVersionStrategyTrivial(t *testing.T) {
	if !clusterVersionRESTStrategies.NamespaceScoped() {
		t.Errorf("cluster version create must be namespace scoped")
	}
	if clusterVersionRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("cluster version should not allow create on update")
	}
	if clusterVersionRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("cluster version should not allow unconditional update")
	}
}

func TestClusterVersionCreate(t *testing.T) {
	// Create a cluster version
	cv := &clusteroperator.ClusterVersion{}

	// Canonicalize the cluster
	clusterVersionRESTStrategies.PrepareForCreate(nil, cv)
}

/*
// TestClusterUpdate tests that generation is incremented correctly when the
// cluster version is updated.
func TestClusterUpdate(t *testing.T) {
	older := clusterWithOldSpec()
	newer := clusterWithOldSpec()

	clusterRESTStrategies.PrepareForUpdate(nil, newer, older)
}
*/
