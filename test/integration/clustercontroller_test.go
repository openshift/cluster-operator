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

// TestClusterCreate tests that creating a cluster creates the node groups
// for the cluster.
func TestClusterCreate(t *testing.T) {
	const sizeOfMasterNodeGroup = 3
	cases := []struct {
		name                  string
		computeNodeGroupNames []string
	}{
		{
			name: "no compute node groups",
		},
		{
			name: "single compute node groups",
			computeNodeGroupNames: []string{
				"first",
			},
		},
		{
			name: "multiple compute node groups",
			computeNodeGroupNames: []string{
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
					MasterNodeGroup: v1alpha1.ClusterNodeGroup{
						Size: sizeOfMasterNodeGroup,
					},
					ComputeNodeGroups: make([]v1alpha1.ClusterComputeNodeGroup, len(tc.computeNodeGroupNames)),
				},
			}

			for i, name := range tc.computeNodeGroupNames {
				cluster.Spec.ComputeNodeGroups[i] = v1alpha1.ClusterComputeNodeGroup{
					Name: name,
					ClusterNodeGroup: v1alpha1.ClusterNodeGroup{
						Size: len(name), // using name length here just to give some variance to the node group size
					},
				}
			}

			clusterOperatorClient.ClusteroperatorV1alpha1().Clusters(testNamespace).Create(cluster)

			if err := waitForClusterToExist(clusterOperatorClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting from Cluster to exist: %v", err)
			}

			if err := waitForClusterToReconcile(clusterOperatorClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to reconcile: %v", err)
			}

			nodeGroups, err := clusterOperatorClient.ClusteroperatorV1alpha1().NodeGroups(testNamespace).List(metav1.ListOptions{})
			if err != nil {
				t.Fatalf("error getting the node groups: %v", err)
			}

			if e, a := 1+len(tc.computeNodeGroupNames), len(nodeGroups.Items); e != a {
				t.Fatalf("unexpected number of node groups: expected %v, got %v", e, a)
			}

			type details struct {
				namePrefix string
				nodeType   v1alpha1.NodeType
				size       int
			}

			expectedDetails := make([]details, 1+len(tc.computeNodeGroupNames))
			expectedDetails[0] = details{
				namePrefix: fmt.Sprintf("%s-master", testClusterName),
				nodeType:   v1alpha1.NodeTypeMaster,
				size:       sizeOfMasterNodeGroup,
			}
			for i, name := range tc.computeNodeGroupNames {
				expectedDetails[1+i] = details{
					namePrefix: fmt.Sprintf("%s-compute-%s", testClusterName, name),
					nodeType:   v1alpha1.NodeTypeCompute,
					size:       len(name),
				}
			}

			actualDetails := make([]details, len(nodeGroups.Items))
			for i, ng := range nodeGroups.Items {
				lastDashInName := strings.LastIndexByte(ng.Name, '-')
				if lastDashInName < 0 {
					t.Fatalf("NodeGroup name does not contain any dashes")
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
				t.Fatalf("unexpected number of node groups: expected %q, got %q", e, a)
			}

			for i, a := range actualDetails {
				e := expectedDetails[i]
				if e != a {
					t.Fatalf("unexpected node group: expected %+v, got %+v", e, a)
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
		clusterOperatorSharedInformers.NodeGroups(),
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
// that its node groups have been created.
func waitForClusterToReconcile(client clientset.Interface, namespace string, name string) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			glog.V(5).Infof("Waiting for cluster %s/%s to reconcile", namespace, name)
			cluster, err := client.ClusteroperatorV1alpha1().Clusters(testNamespace).Get(testClusterName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			if cluster.Status.MasterNodeGroups == 1 && cluster.Status.ComputeNodeGroups == len(cluster.Spec.ComputeNodeGroups) {
				return true, nil
			}
			return false, nil
		},
	)
}
