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

package nodelink

import (
	"testing"

	"github.com/golang/mock/gomock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	clustopv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientfake "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

const (
	testMachineName           = "testmachine"
	testNamespace             = "test-namespace"
	testClusterDeploymentName = "test-cluster-deployment"
	testClusterDeploymentUUID = types.UID("test-cluster-deployment-uuid")
	testClusterID             = "test-cluster-id"
	testRegion                = "us-east-1"
	testClusterVerName        = "v3-10"
	testClusterVerNS          = "cluster-operator"
	testClusterVerUID         = types.UID("test-cluster-version")
	testImage                 = "testAMI"
)

func testTaint(key, value string) corev1.Taint {
	return corev1.Taint{
		Key:    key,
		Value:  value,
		Effect: corev1.TaintEffectNoExecute,
	}
}

func TestNodeLinker(t *testing.T) {
	cases := []struct {
		name                string
		nodeUpdatedExpected bool
		machines            []*capiv1.Machine
		node                *corev1.Node
		expectedAnnotations map[string]string
		expectedLabels      map[string]string
		expectedError       string
		expectedTaints      []corev1.Taint
	}{
		{
			name:                "initial linking",
			nodeUpdatedExpected: true,
			machines: []*capiv1.Machine{
				testMachine(1, "machine1", "10.0.0.1",
					map[string]string{"a": "1"},
					[]corev1.Taint{
						testTaint("c", "3"),
						testTaint("d", "4"),
					}),
				testMachine(1, "machine2", "10.0.0.2",
					map[string]string{"b": "2"}, []corev1.Taint{}),
			},
			node: testNode("10.0.0.1", "", map[string]string{}, nil, map[string]string{}),
			expectedAnnotations: map[string]string{
				"machine": "kube-cluster/machine1",
			},
			expectedLabels: map[string]string{
				"a": "1",
			},
			expectedTaints: []corev1.Taint{
				testTaint("c", "3"),
				testTaint("d", "4"),
			},
		},
		{
			name:                "no matching machine",
			nodeUpdatedExpected: false,
			machines: []*capiv1.Machine{
				testMachine(1, "machine1", "10.0.0.1", map[string]string{}, []corev1.Taint{}),
				testMachine(1, "machine2", "10.0.0.2", map[string]string{}, []corev1.Taint{}),
			},
			node:          testNode("10.0.0.5", "", map[string]string{}, nil, map[string]string{}),
			expectedError: "no matching machine found for node",
		},
		{
			// Not sure if this is possible but just in case we test that if a machine matching
			// the nodes annotation is deleted, a new machine may match it's IP and should then
			// be linked.
			name:                "changed matching machine",
			nodeUpdatedExpected: true,
			machines: []*capiv1.Machine{
				testMachine(1, "machine1", "10.0.0.1",
					map[string]string{"a": "1"},
					[]corev1.Taint{
						testTaint("c", "3"),
						testTaint("d", "4"),
					}),
				testMachine(1, "machine2", "10.0.0.2",
					map[string]string{"b": "2"}, []corev1.Taint{}),
			},
			node: testNode("10.0.0.1", "", map[string]string{}, nil, map[string]string{"machine": "kube-cluster/sincedeletedmachine"}),
			expectedAnnotations: map[string]string{
				"machine": "kube-cluster/machine1",
			},
			expectedLabels: map[string]string{
				"a": "1",
			},
			expectedTaints: []corev1.Taint{
				testTaint("c", "3"),
				testTaint("d", "4"),
			},
		},
		{
			// Not sure if this is possible but just in case we test that if a machine matching
			// the nodes annotation is deleted, a new machine may match it's IP and should then
			// be linked.
			name:                "matching machine deleted",
			nodeUpdatedExpected: false,
			machines: []*capiv1.Machine{
				testMachine(1, "machine1", "10.0.0.1",
					map[string]string{"a": "1"},
					[]corev1.Taint{
						testTaint("c", "3"),
						testTaint("d", "4"),
					}),
			},
			node:          testNode("10.0.0.7", "", map[string]string{}, nil, map[string]string{"machine": "kube-cluster/sincedeletedmachine"}),
			expectedError: "no matching machine found for node",
		},
		{
			name:                "no changes required",
			nodeUpdatedExpected: false,
			machines: []*capiv1.Machine{
				testMachine(1, "machine1", "10.0.0.1",
					map[string]string{"a": "1"},
					[]corev1.Taint{
						testTaint("c", "3"),
						testTaint("d", "4"),
					}),
				testMachine(1, "machine2", "10.0.0.2", map[string]string{}, []corev1.Taint{}),
			},
			node: testNode("10.0.0.1", "kube-cluster/machine1", map[string]string{"a": "1"},
				[]corev1.Taint{
					testTaint("c", "3"),
					testTaint("d", "4"),
				},
				map[string]string{},
			),
		},
		{
			name:                "taints fully replaced",
			nodeUpdatedExpected: true,
			machines: []*capiv1.Machine{
				testMachine(1, "machine1", "10.0.0.1",
					map[string]string{"a": "1"},
					[]corev1.Taint{
						testTaint("c", "3"),
						testTaint("d", "4"),
					}),
				testMachine(1, "machine2", "10.0.0.2",
					map[string]string{"b": "2"}, []corev1.Taint{}),
			},
			node: testNode("10.0.0.1", "kube-cluster/machine1", map[string]string{},
				[]corev1.Taint{
					testTaint("e", "5"),
				},
				map[string]string{},
			),
			expectedAnnotations: map[string]string{
				"machine": "kube-cluster/machine1",
			},
			expectedLabels: map[string]string{
				"a": "1",
			},
			expectedTaints: []corev1.Taint{
				testTaint("c", "3"),
				testTaint("d", "4"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			kubeClient := &clientgofake.Clientset{}
			capiClient := &capiclientfake.Clientset{}
			capiInformers := capiinformers.NewSharedInformerFactory(capiClient, 0)
			coreInformers := informers.NewSharedInformerFactory(kubeClient, 0)
			machineStore := capiInformers.Cluster().V1alpha1().Machines().Informer().GetStore()
			for _, m := range tc.machines {
				machineStore.Add(m)
			}

			ctrlr := NewController(coreInformers.Core().V1().Nodes(),
				capiInformers.Cluster().V1alpha1().Machines(),
				kubeClient, capiClient)

			err := ctrlr.processNode(tc.node)
			if tc.expectedError != "" {
				assert.Contains(t, err.Error(), tc.expectedError)
			} else {
				assert.NoError(t, err)
			}

			if tc.nodeUpdatedExpected {
				if assert.Equal(t, 1, len(kubeClient.Actions())) {
					action := kubeClient.Actions()[0]
					assert.Equal(t, "update", action.GetVerb())
					updateAction, ok := action.(clientgotesting.UpdateAction)
					assert.True(t, ok)
					updatedObject := updateAction.GetObject()

					node, ok := updatedObject.(*corev1.Node)
					for k, v := range tc.expectedAnnotations {
						assert.Equal(t, v, node.Annotations[k])
					}
					for k, v := range tc.expectedLabels {
						assert.Equal(t, v, node.Labels[k])
					}
					if tc.expectedTaints != nil {
						assert.ElementsMatch(t, tc.expectedTaints, node.Spec.Taints)
					}
				}
			} else {
				assert.Equal(t, 0, len(kubeClient.Actions()))
			}

		})
	}
}

func testMachine(generation int64, name, internalIP string, labels map[string]string, taints []corev1.Taint) *capiv1.Machine {
	testAMI := testImage
	msSpec := clustopv1.MachineSetSpec{
		MachineSetConfig: clustopv1.MachineSetConfig{
			Infra:    false,
			Size:     3,
			NodeType: clustopv1.NodeTypeCompute,
			Hardware: &clustopv1.MachineSetHardwareSpec{
				AWS: &clustopv1.MachineSetAWSHardwareSpec{
					InstanceType: "t2.micro",
				},
			},
		},
		ClusterHardware: clustopv1.ClusterHardwareSpec{
			AWS: &clustopv1.AWSClusterSpec{
				Region: testRegion,
			},
		},
		VMImage: clustopv1.VMImage{
			AWSImage: &testAMI,
		},
	}
	rawProviderConfig, _ := controller.MachineProviderConfigFromMachineSetSpec(&msSpec)
	machine := &capiv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  kubeClusterNamespace,
			Generation: generation,
			Labels: map[string]string{
				clustopv1.ClusterNameLabel:       testClusterID,
				clustopv1.ClusterDeploymentLabel: testClusterDeploymentName,
			},
		},
		Spec: capiv1.MachineSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Taints: taints,
			ProviderConfig: capiv1.ProviderConfig{
				Value: rawProviderConfig,
			},
		},
		Status: capiv1.MachineStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: internalIP,
				},
			},
		},
	}
	return machine
}

func testNode(internalIP, machineAnnotation string, labels map[string]string, taints []corev1.Taint, annotations map[string]string) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testnode",
			Annotations: map[string]string{},
			Labels:      labels,
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: internalIP,
				},
			},
		},
	}
	if machineAnnotation != "" {
		node.Annotations["machine"] = machineAnnotation
	}
	if taints != nil {
		node.Spec.Taints = taints
	}
	return node
}

// testClusterDeployment creates a new test ClusterDeployment
func testClusterDeployment() *clustopv1.ClusterDeployment {
	clusterDeployment := &clustopv1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterDeploymentUUID,
			Name:      testClusterDeploymentName,
			Namespace: testNamespace,
		},
		Spec: clustopv1.ClusterDeploymentSpec{
			ClusterName: testClusterID,
			MachineSets: []clustopv1.ClusterMachineSet{
				{
					ShortName: "",
					MachineSetConfig: clustopv1.MachineSetConfig{
						Infra:    true,
						Size:     1,
						NodeType: clustopv1.NodeTypeMaster,
					},
				},
				{
					ShortName: "compute",
					MachineSetConfig: clustopv1.MachineSetConfig{
						Infra:    false,
						Size:     1,
						NodeType: clustopv1.NodeTypeCompute,
					},
				},
			},
			Hardware: clustopv1.ClusterHardwareSpec{
				AWS: &clustopv1.AWSClusterSpec{
					SSHUser:     "clusteroperator",
					Region:      testRegion,
					KeyPairName: "libra",
				},
			},
			ClusterVersionRef: clustopv1.ClusterVersionReference{
				Name:      testClusterVerName,
				Namespace: testClusterVerNS,
			},
		},
	}
	return clusterDeployment
}

func testCluster(t *testing.T) *capiv1.Cluster {
	clusterDeployment := testClusterDeployment()
	cluster, err := controller.BuildCluster(clusterDeployment, testClusterVersion().Spec)
	assert.NoError(t, err)
	return cluster
}

// testClusterVersion will create a ClusterVersion resource.
func testClusterVersion() *clustopv1.ClusterVersion {
	cv := &clustopv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterVerUID,
			Name:      testClusterVerName,
			Namespace: testClusterVerNS,
		},
		Spec: clustopv1.ClusterVersionSpec{
			Images: clustopv1.ClusterVersionImages{
				ImageFormat: "openshift/origin-${component}:${version}",
			},
			VMImages: clustopv1.VMImages{
				AWSImages: &clustopv1.AWSVMImages{
					RegionAMIs: []clustopv1.AWSRegionAMIs{
						{
							Region: testRegion,
							AMI:    "computeAMI_ID",
						},
					},
				},
			},
		},
	}
	return cv
}
