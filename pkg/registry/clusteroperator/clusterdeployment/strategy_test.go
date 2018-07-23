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

	"github.com/stretchr/testify/assert"

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

// TestClusterDeploymentCreateWithSshKeyName
func TestClusterDeploymentCreateWithSshKeyName(t *testing.T) {
	cases := []struct {
		name                          string
		clusterDeploymentName         string
		keyPairName                   string
		keyPairNameGenerationExpected bool
	}{
		{
			name: "specify ssh keypair name",
			clusterDeploymentName:         "testcluster",
			keyPairName:                   "TEST KEYPAIR NAME",
			keyPairNameGenerationExpected: false,
		},
		{
			name: "generate ssh keypair name",
			clusterDeploymentName:         "testcluster",
			keyPairNameGenerationExpected: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			// Create a clusterdeployment or clusterdeployments
			clusterDeployment := &clusteroperator.ClusterDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: tc.clusterDeploymentName,
				},
				Spec: clusteroperator.ClusterDeploymentSpec{
					Hardware: clusteroperator.ClusterHardwareSpec{
						AWS: &clusteroperator.AWSClusterSpec{
							KeyPairName: tc.keyPairName,
						},
					},
				},
			}

			// Act
			// Canonicalize the cluster
			clusterDeploymentRESTStrategies.PrepareForCreate(nil, clusterDeployment)

			// Assert
			assert.Regexp(t, "^testcluster-\\w\\w\\w\\w\\w$", clusterDeployment.Spec.ClusterName)

			if tc.keyPairNameGenerationExpected {
				assert.Equal(t, clusterDeployment.Spec.ClusterName, clusterDeployment.Spec.Hardware.AWS.KeyPairName)
			} else {
				assert.Equal(t, tc.keyPairName, clusterDeployment.Spec.Hardware.AWS.KeyPairName)
			}
		})
	}
}

// TestClusterDeploymentUpdate tests that generation is incremented correctly when the
// spec of a Cluster is updated.
func TestClusterDeploymentUpdate(t *testing.T) {
	older := clusterDeploymentWithOldSpec()
	newer := clusterDeploymentWithNewSpec()

	clusterDeploymentRESTStrategies.PrepareForUpdate(nil, newer, older)
}
