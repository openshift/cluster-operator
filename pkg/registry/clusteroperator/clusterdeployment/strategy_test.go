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

package clusterdeployment

import (
	"testing"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clusterDeploymentWithOldSpec() *clusteroperator.ClusterDeployment {
	return &clusteroperator.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
}

func clusterDeploymentWithNewSpec() *clusteroperator.ClusterDeployment {
	b := clusterDeploymentWithOldSpec()
	return b
}

// TestClusterDeploymentStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestClusterDeploymentStrategyTrivial(t *testing.T) {
	if !clusterDeploymentRESTStrategies.NamespaceScoped() {
		t.Errorf("clusterseployment create must be namespace scoped")
	}
	if clusterDeploymentRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("clusterseployment should not allow create on update")
	}
	if clusterDeploymentRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("clusterseployment should not allow unconditional update")
	}
}

// TestClusterDeploymentCreate
func TestClusterDeploymentCreate(t *testing.T) {
	// Create a clusterdeployment or clusterdeployments
	clusterDeployment := &clusteroperator.ClusterDeployment{}

	// Canonicalize the cluster
	clusterDeploymentRESTStrategies.PrepareForCreate(nil, clusterDeployment)
}

// TestClusterDeploymentUpdate tests that generation is incremented correctly when the
// spec of a Cluster is updated.
func TestClusterDeploymentUpdate(t *testing.T) {
	older := clusterDeploymentWithOldSpec()
	newer := clusterDeploymentWithNewSpec()

	clusterDeploymentRESTStrategies.PrepareForUpdate(nil, newer, older)
}
