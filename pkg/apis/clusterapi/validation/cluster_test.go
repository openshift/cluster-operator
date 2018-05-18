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

package validation

import (
	"bytes"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	clusteroperatorapi "github.com/openshift/cluster-operator/pkg/api"
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster"
)

func getValidClusterProviderConfig() *clusteroperator.ClusterProviderConfigSpec {
	spec := &clusteroperator.ClusterProviderConfigSpec{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterProviderConfigSpec",
			APIVersion: "clusteroperator.openshift.io/v1alpha1",
		},
		ClusterSpec: clusteroperator.ClusterSpec{
			ClusterVersionRef: clusteroperator.ClusterVersionReference{
				Name: "someclusterref",
			},
			MachineSets: []clusteroperator.ClusterMachineSet{
				{
					ShortName: "master",
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: "Master",
						Size:     1,
					},
				},
				{
					ShortName: "infra",
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: "Compute",
						Infra:    true,
						Size:     1,
					},
				},
			},
		},
	}
	return spec
}

func getValidClusterWithEmptyProviderConfig() *cluster.Cluster {
	cluster := &cluster.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
	}
	return cluster
}

func getRawExtensionFromProviderConfig(providerConfig *clusteroperator.ClusterProviderConfigSpec, t *testing.T) *runtime.RawExtension {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, clusteroperatorapi.Scheme, clusteroperatorapi.Scheme, false)
	var buffer bytes.Buffer
	err := serializer.Encode(providerConfig, &buffer)
	if err != nil {
		t.Fatalf("error encoding providerconfig %v", err)
	}

	return &runtime.RawExtension{
		Raw: buffer.Bytes(),
	}
}

func getValidCluster(t *testing.T) *cluster.Cluster {
	cluster := getValidClusterWithEmptyProviderConfig()
	providerConfig := getValidClusterProviderConfig()

	cluster.Spec.ProviderConfig.Value = getRawExtensionFromProviderConfig(providerConfig, t)

	return cluster
}

func getValidClusterStrategy() *ClusterStrategy {
	return &ClusterStrategy{}
}

// TestValidateCluster tests the Validate function.
func TestValidateCluster(t *testing.T) {
	cases := []struct {
		name    string
		cluster *cluster.Cluster
		valid   bool
	}{
		{
			name:    "valid",
			cluster: getValidCluster(t),
			valid:   true,
		},
		{
			name: "invalid name",
			cluster: func() *cluster.Cluster {
				c := getValidCluster(t)
				c.Name = "###"
				return c
			}(),
			valid: false,
		},
		/*
			{
				name: "provider config without machinesets",
				cluster: func() *cluster.Cluster {
					c := getValidClusterWithEmptyProviderConfig()
					pc := getValidClusterProviderConfig()
					pc.MachineSets = []clusteroperator.ClusterMachineSet{}
					c.Spec.ProviderConfig.Value = getRawExtensionFromProviderConfig(pc, t)

					return c
				}(),
				valid: true,
			},
		*/
		{
			name: "invalid spec",
			cluster: func() *cluster.Cluster {
				c := getValidClusterWithEmptyProviderConfig()
				pc := getValidClusterProviderConfig()
				pc.MachineSets[0].Size = 0
				c.Spec.ProviderConfig.Value = getRawExtensionFromProviderConfig(pc, t)
				return c
			}(),
			valid: false,
		},
	}

	clusterStrategy := &ClusterStrategy{}
	ctx := request.NewContext()
	if ctx == nil {
		t.Fatalf("problem creating context")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errorlist := clusterStrategy.Validate(ctx, tc.cluster)

			if len(errorlist) != 0 && tc.valid {
				t.Errorf("unexpexted error %v", errorlist)
			} else if len(errorlist) == 0 && !tc.valid {
				t.Errorf("unexpected success")
			}
		})
		/*
			errorlist := clusterStrategy.Validate(ctx, tc.cluster)

			if len(errorlist) != 0 && tc.valid {
				t.Errorf("%v: unexpected error %v", tc.name, errorlist)
			} else if len(errorlist) == 0 && !tc.valid {
				t.Errorf("%v: unexpected success", tc.name)
			}
		*/
	}
}
