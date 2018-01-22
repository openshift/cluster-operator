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

package integration

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"

	// avoid error `clusteroperator/v1alpha1 is not enabled`
	_ "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/install"

	"github.com/golang/glog"

	servertesting "github.com/openshift/cluster-operator/cmd/cluster-operator-apiserver/app/testing"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	coinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	clustercontroller "github.com/openshift/cluster-operator/pkg/controller/cluster"
)

const (
	testNamespace   = "test-namespace"
	testClusterName = "test-cluster"
)

// TestClusterCreate tests that creating a cluster creates the machine sets
// for the cluster.
func TestClusterCreate(t *testing.T) {
	const sizeOfMasterMachineSet = 3
	cases := []struct {
		name                   string
		computeMachineSetNames []string
	}{
		{
			name: "no compute machine sets",
		},
		{
			name: "single compute machine sets",
			computeMachineSetNames: []string{
				"first",
			},
		},
		{
			name: "multiple compute machine sets",
			computeMachineSetNames: []string{
				"first",
				"second",
				"3rd",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, clusterOperatorClient, tearDown := startServerAndClusterController(t)
			defer tearDown()

			cluster := &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: testClusterName,
				},
				Spec: v1alpha1.ClusterSpec{
					Version: v1alpha1.ClusterVersionReference{
						Name: "v3-9",
					},
					MachineSets: []v1alpha1.ClusterMachineSet{
						{
							Name: "master",
							MachineSetConfig: v1alpha1.MachineSetConfig{
								NodeType: v1alpha1.NodeTypeMaster,
								Infra:    true,
								Size:     sizeOfMasterMachineSet,
							},
						},
					},
				},
			}

			for _, name := range tc.computeMachineSetNames {
				cluster.Spec.MachineSets = append(cluster.Spec.MachineSets, v1alpha1.ClusterMachineSet{
					Name: name,
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeCompute,
						Size:     len(name), // using name length here just to give some variance to the machine set size
					},
				})
			}

			clusterOperatorClient.ClusteroperatorV1alpha1().Clusters(testNamespace).Create(cluster)

			if err := waitForClusterToExist(clusterOperatorClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to exist: %v", err)
			}

			if err := waitForClusterToReconcile(clusterOperatorClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to reconcile: %v", err)
			}

			machineSets, err := clusterOperatorClient.ClusteroperatorV1alpha1().MachineSets(testNamespace).List(metav1.ListOptions{})
			if err != nil {
				t.Fatalf("error getting the machine sets: %v", err)
			}

			if e, a := 1+len(tc.computeMachineSetNames), len(machineSets.Items); e != a {
				t.Fatalf("unexpected number of machine sets: expected %v, got %v", e, a)
			}

			type details struct {
				namePrefix string
				nodeType   v1alpha1.NodeType
				size       int
			}

			expectedDetails := make([]details, 1+len(tc.computeMachineSetNames))
			expectedDetails[0] = details{
				namePrefix: fmt.Sprintf("%s-master", testClusterName),
				nodeType:   v1alpha1.NodeTypeMaster,
				size:       sizeOfMasterMachineSet,
			}
			for i, name := range tc.computeMachineSetNames {
				expectedDetails[1+i] = details{
					namePrefix: fmt.Sprintf("%s-%s", testClusterName, name),
					nodeType:   v1alpha1.NodeTypeCompute,
					size:       len(name),
				}
			}

			actualDetails := make([]details, len(machineSets.Items))
			for i, ng := range machineSets.Items {
				lastDashInName := strings.LastIndexByte(ng.Name, '-')
				if lastDashInName < 0 {
					t.Fatalf("MachineSet name does not contain any dashes")
				}
				actualDetails[i] = details{
					namePrefix: ng.Name[:lastDashInName],
					nodeType:   ng.Spec.NodeType,
					size:       ng.Spec.Size,
				}
			}

			sortDetails := func(detailsSlice []details) {
				sort.Slice(detailsSlice, func(i, j int) bool {
					return detailsSlice[i].namePrefix < detailsSlice[j].namePrefix
				})
			}
			sortDetails(expectedDetails)
			sortDetails(actualDetails)

			if e, a := len(expectedDetails), len(actualDetails); e != a {
				t.Fatalf("unexpected number of machine sets: expected %q, got %q", e, a)
			}

			for i, a := range actualDetails {
				e := expectedDetails[i]
				if e != a {
					t.Fatalf("unexpected machine set: expected %+v, got %+v", e, a)
				}
			}
		})
	}
}

// startServerAndClusterController creates a new test ClusterController injected with
// fake clients and returns:
//
// - a fake kubernetes core api client
// - a cluster-operator api client
// - a function for shutting down the controller and API server
//
// If there is an error, startServerAndClusterController calls 'Fatal' on the
// injected testing.T.
func startServerAndClusterController(t *testing.T) (
	*fake.Clientset,
	clientset.Interface,
	func()) {

	// create a fake kube client
	fakeKubeClient := &fake.Clientset{}

	// create a cluster-operator client and running server
	clusterOperatorClientConfig, shutdownServer := servertesting.StartTestServerOrDie(t)
	clusterOperatorClient, err := clientset.NewForConfig(clusterOperatorClientConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// create informers
	informerFactory := coinformers.NewSharedInformerFactory(clusterOperatorClient, 10*time.Second)
	clusterOperatorSharedInformers := informerFactory.Clusteroperator().V1alpha1()

	// create a test controller
	testController := clustercontroller.NewClusterController(
		clusterOperatorSharedInformers.Clusters(),
		clusterOperatorSharedInformers.MachineSets(),
		fakeKubeClient,
		clusterOperatorClient,
	)
	t.Log("controller start")

	stopCh := make(chan struct{})
	controllerStopped := make(chan struct{})
	go func() {
		testController.Run(1, stopCh)
		controllerStopped <- struct{}{}
	}()
	informerFactory.Start(stopCh)
	t.Log("informers start")

	shutdown := func() {
		// Shut down controller
		close(stopCh)
		<-controllerStopped
		// Shut down api server
		shutdownServer()
	}

	return fakeKubeClient, clusterOperatorClient, shutdown
}

// waitForClusterToExist waits for the Cluster with the specified name to
// exist in the specified namespace.
func waitForClusterToExist(client clientset.Interface, namespace string, name string) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			glog.V(5).Infof("Waiting for cluster %s/%s to exist", namespace, name)
			_, err := client.ClusteroperatorV1alpha1().Clusters(namespace).Get(name, metav1.GetOptions{})
			if nil == err {
				return true, nil
			}

			return false, nil
		},
	)
}

// waitForClusterToReconcile waits for reconciliation to complete on the cluster.
// A completed reconciliation means that the status of the cluster indicates
// that its machine sets have been created.
func waitForClusterToReconcile(client clientset.Interface, namespace string, name string) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			glog.V(5).Infof("Waiting for cluster %s/%s to reconcile", namespace, name)
			cluster, err := client.ClusteroperatorV1alpha1().Clusters(testNamespace).Get(testClusterName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			if cluster.Status.MachineSetCount == len(cluster.Spec.MachineSets) {
				return true, nil
			}
			return false, nil
		},
	)
}
