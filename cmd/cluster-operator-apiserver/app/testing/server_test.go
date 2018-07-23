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

package testing

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/typed/clusteroperator/v1alpha1"
)

func TestRun(t *testing.T) {
	config, tearDown := StartTestServerOrDie(t)
	defer tearDown()

	client, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// test whether the server is really healthy after /healthz told us so
	t.Logf("Creating ClusterDeployment directly after being healthy")
	_, err = client.ClusterDeployments("default").Create(&clusteroperator.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Spec: clusteroperator.ClusterDeploymentSpec{
			ClusterVersionRef: clusteroperator.ClusterVersionReference{
				Namespace: "openshift-cluster-operator",
				Name:      "v3-9",
			},
			MachineSets: []clusteroperator.ClusterMachineSet{
				{
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: clusteroperator.NodeTypeMaster,
						Infra:    true,
						Size:     1,
					},
				},
			},
			Hardware: clusteroperator.ClusterHardwareSpec{
				AWS: &clusteroperator.AWSClusterSpec{},
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create clusterdeployment: %v", err)
	}
}
