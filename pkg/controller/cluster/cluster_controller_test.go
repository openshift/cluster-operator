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
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	"github.com/openshift/cluster-operator/pkg/controller"
)

const (
	testNamespace   = "test-namespace"
	testClusterName = "test-cluster"
	testClusterUUID = types.UID("test-cluster-uuid")
)

// newTestClusterController creates a test ClusterController with fake
// clients and informers.
func newTestClusterController() (
	*ClusterController,
	cache.Store, // cluster store
	cache.Store, // node group store
	*clientgofake.Clientset,
	*clusteroperatorclientset.Clientset,
) {
	kubeClient := &clientgofake.Clientset{}
	clusterOperatorClient := &clusteroperatorclientset.Clientset{}
	informers := informers.NewSharedInformerFactory(clusterOperatorClient, 0)

	controller := NewClusterController(
		informers.Clusteroperator().V1alpha1().Clusters(),
		informers.Clusteroperator().V1alpha1().NodeGroups(),
		kubeClient,
		clusterOperatorClient,
	)

	controller.clustersSynced = alwaysReady
	controller.nodeGroupsSynced = alwaysReady

	return controller,
		informers.Clusteroperator().V1alpha1().Clusters().Informer().GetStore(),
		informers.Clusteroperator().V1alpha1().NodeGroups().Informer().GetStore(),
		kubeClient,
		clusterOperatorClient
}

// alwaysReady is a function that can be used as a sync function that will
// always indicate that the lister has been synced.
var alwaysReady = func() bool { return true }

// getKey gets the key for the cluster to use when checking expectations
// set on a cluster.
func getKey(cluster *clusteroperator.Cluster, t *testing.T) string {
	if key, err := controller.KeyFunc(cluster); err != nil {
		t.Errorf("Unexpected error getting key for Cluster %v: %v", cluster.Name, err)
		return ""
	} else {
		return key
	}
}

// newCluster creates a new cluster with compute node groups that have
// the specified names.
func newCluster(computeNames ...string) *clusteroperator.Cluster {
	computes := make([]clusteroperator.ClusterComputeNodeGroup, len(computeNames))
	for i, computeName := range computeNames {
		computes[i] = clusteroperator.ClusterComputeNodeGroup{
			Name: computeName,
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
		}
	}
	return newClusterWithSizes(1, computes...)
}

// newClusterWithSizes creates a new cluster with the specified size for the
// master node group and the specified cluster compute node groups.
func newClusterWithSizes(masterSize int, computes ...clusteroperator.ClusterComputeNodeGroup) *clusteroperator.Cluster {
	cluster := &clusteroperator.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterUUID,
			Name:      testClusterName,
			Namespace: testNamespace,
		},
		Spec: clusteroperator.ClusterSpec{
			MasterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: masterSize,
			},
			ComputeNodeGroups: computes,
		},
	}
	return cluster
}

// newNodeGroup creates a new node group.
func newNodeGroup(name string, cluster *clusteroperator.Cluster, properlyOwned bool) *clusteroperator.NodeGroup {
	var controllerReference metav1.OwnerReference
	if properlyOwned {
		trueVar := true
		controllerReference = metav1.OwnerReference{
			UID:        testClusterUUID,
			APIVersion: clusteroperator.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			Controller: &trueVar,
		}
	}
	return &clusteroperator.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{controllerReference},
		},
	}
}

// newNodeGroups creates new node groups and stores them in the specified
// store.
func newNodeGroups(store cache.Store, cluster *clusteroperator.Cluster, includeMaster bool, computeNames ...string) []*clusteroperator.NodeGroup {
	masterSize := 0
	if includeMaster {
		masterSize = 1
	}

	computes := make([]clusteroperator.ClusterComputeNodeGroup, len(computeNames))
	for i, computeName := range computeNames {
		computes[i] = clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
			Name: computeName,
		}
	}

	return newNodeGroupsWithSizes(store, cluster, masterSize, computes...)
}

// newNodeGroupsWithSizes creates new node groups, with specific sizes, and
// stores them in the specified store.
func newNodeGroupsWithSizes(store cache.Store, cluster *clusteroperator.Cluster, masterSize int, computes ...clusteroperator.ClusterComputeNodeGroup) []*clusteroperator.NodeGroup {
	nodeGroups := []*clusteroperator.NodeGroup{}
	if masterSize > 0 {
		name := fmt.Sprintf("%s-master-random", cluster.Name)
		nodeGroup := newNodeGroup(name, cluster, true)
		nodeGroup.Spec.NodeType = clusteroperator.NodeTypeMaster
		nodeGroup.Spec.Size = masterSize
		nodeGroups = append(nodeGroups, nodeGroup)
	}
	for _, compute := range computes {
		name := fmt.Sprintf("%s-compute-%s-random", cluster.Name, compute.Name)
		nodeGroup := newNodeGroup(name, cluster, true)
		nodeGroup.Spec.NodeType = clusteroperator.NodeTypeCompute
		nodeGroup.Spec.Size = compute.Size
		nodeGroups = append(nodeGroups, nodeGroup)
	}
	if store != nil {
		for _, nodeGroup := range nodeGroups {
			store.Add(nodeGroup)
		}
	}
	return nodeGroups

}

// processSync initiates a sync via processNextWorkItem() to test behavior that
// depends on both functions (such as re-queueing on sync error).
func processSync(c *ClusterController, key string) error {
	// Save old syncHandler and replace with one that captures the error.
	oldSyncHandler := c.syncHandler
	defer func() {
		c.syncHandler = oldSyncHandler
	}()
	var syncErr error
	c.syncHandler = func(key string) error {
		syncErr = oldSyncHandler(key)
		return syncErr
	}
	c.queue.Add(key)
	c.processNextWorkItem()
	return syncErr
}

// validateClientActions validates that the client experienced the specified
// expected actions.
func validateClientActions(t *testing.T, clusterOperatorClient *clusteroperatorclientset.Clientset, expectedActions ...expectedClientAction) {
	actualActions := clusterOperatorClient.Actions()
	if e, a := len(expectedActions), len(actualActions); e != a {
		t.Errorf("unexpected number of client actions: expected %v, got %v", e, a)
	}
	expectedActionSatisfied := make([]bool, len(expectedActions))
	for _, actualAction := range actualActions {
		actualActionSatisfied := false
		for i, expectedAction := range expectedActions {
			if expectedActionSatisfied[i] {
				continue
			}
			if actualAction.GetResource() != expectedAction.resource() {
				continue
			}
			if actualAction.GetVerb() != expectedAction.verb() {
				continue
			}
			if expectedAction.validate(t, actualAction) {
				actualActionSatisfied = true
				expectedActionSatisfied[i] = true
				break
			}
		}
		if !actualActionSatisfied {
			t.Errorf("Unexpected client action: %+v", actualAction)
		}
	}
}

// expectedClientAction is an action that is expected on the client.
type expectedClientAction interface {
	// resource gets the resource for the action
	resource() schema.GroupVersionResource
	// verb gets the verb for the action
	verb() string
	// validate validates that the action performed meets the expectation.
	// return true if the action is a match for the expectation, whether
	// valid or not.
	validate(t *testing.T, action clientgotesting.Action) bool
}

// expectedNodeGroupCreateAction is an expected client action to create a
// node group.
type expectedNodeGroupCreateAction struct {
	namePrefix string
}

func (ea expectedNodeGroupCreateAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("nodegroups")
}

func (ea expectedNodeGroupCreateAction) verb() string {
	return "create"
}

func (ea expectedNodeGroupCreateAction) validate(t *testing.T, action clientgotesting.Action) bool {
	createAction, ok := action.(clientgotesting.CreateAction)
	if !ok {
		t.Errorf("create action is not a CreateAction: %t", createAction)
		return false
	}
	createdObject := createAction.GetObject()
	nodeGroup, ok := createdObject.(*clusteroperator.NodeGroup)
	if !ok {
		t.Errorf("node group create action object is not a NodeGroup: %t", nodeGroup)
		return false
	}
	return nodeGroup.GenerateName == ea.namePrefix
}

// newExpectedMasterNodeGroupCreateAction creates a new expected client
// action for creating a master node group.
func newExpectedMasterNodeGroupCreateAction(cluster *clusteroperator.Cluster) expectedNodeGroupCreateAction {
	return expectedNodeGroupCreateAction{
		namePrefix: getNamePrefixForMasterNodeGroup(cluster),
	}
}

// newExpectedComputeNodeGroupCreateAction creates a new expected client
// action for creating a compute node group.
func newExpectedComputeNodeGroupCreateAction(cluster *clusteroperator.Cluster, computeName string) expectedNodeGroupCreateAction {
	return expectedNodeGroupCreateAction{
		namePrefix: getNamePrefixForComputeNodeGroup(cluster, computeName),
	}
}

// expectedNodeGroupDeleteAction is an expected client action to delete a
// node group.
type expectedNodeGroupDeleteAction struct {
	namePrefix string
}

func (ea expectedNodeGroupDeleteAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("nodegroups")
}

func (ea expectedNodeGroupDeleteAction) verb() string {
	return "delete"
}

func (ea expectedNodeGroupDeleteAction) validate(t *testing.T, action clientgotesting.Action) bool {
	deleteAction, ok := action.(clientgotesting.DeleteAction)
	if !ok {
		t.Errorf("delete action is not a DeleteAction: %t", deleteAction)
		return false
	}
	return strings.HasPrefix(deleteAction.GetName(), ea.namePrefix)
}

// newExpectedMasterNodeGroupDeleteAction creates a new expected client
// action for deleting a master node group.
func newExpectedMasterNodeGroupDeleteAction(cluster *clusteroperator.Cluster) expectedNodeGroupDeleteAction {
	return expectedNodeGroupDeleteAction{
		namePrefix: getNamePrefixForMasterNodeGroup(cluster),
	}
}

// newExpectedComputeNodeGroupDeleteAction creates a new expected client
// action for deleting a compute node group.
func newExpectedComputeNodeGroupDeleteAction(cluster *clusteroperator.Cluster, computeName string) expectedNodeGroupDeleteAction {
	return expectedNodeGroupDeleteAction{
		namePrefix: getNamePrefixForComputeNodeGroup(cluster, computeName),
	}
}

// expectedClusterStatusUpdateAction is an expected client action to update
// the status of a cluster.
type expectedClusterStatusUpdateAction struct {
	masterNodeGroups  int
	computeNodeGroups int
}

func (ea expectedClusterStatusUpdateAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("clusters")
}

func (ea expectedClusterStatusUpdateAction) verb() string {
	return "update"
}

func (ea expectedClusterStatusUpdateAction) validate(t *testing.T, action clientgotesting.Action) bool {
	if action.GetSubresource() != "status" {
		return false
	}
	updateAction, ok := action.(clientgotesting.UpdateAction)
	if !ok {
		t.Errorf("update action is not an UpdateAction: %t", updateAction)
		return false
	}
	updatedObject := updateAction.GetObject()
	cluster, ok := updatedObject.(*clusteroperator.Cluster)
	if !ok {
		t.Errorf("cluster status update action object is not a Cluster: %t", cluster)
		return true
	}
	if e, a := ea.masterNodeGroups, cluster.Status.MasterNodeGroups; e != a {
		t.Errorf("unexpected masterNodeGroups in cluster update status: expected %v, got %v", e, a)
	}
	if e, a := ea.computeNodeGroups, cluster.Status.ComputeNodeGroups; e != a {
		t.Errorf("unexpected computeNodeGroups in cluster update status: expected %v, got %v", e, a)
	}
	return true
}

// validateControllerExpectations validates that the specified cluster is
// expecting the specified number of creations and deletions of node groups.
func validateControllerExpectations(t *testing.T, ctrlr *ClusterController, cluster *clusteroperator.Cluster, expectedAdds, expectedDeletes int) {
	expectations, ok, err := ctrlr.expectations.GetExpectations(getKey(cluster, t))
	switch {
	case err != nil:
		t.Errorf("error getting expecations: %v", cluster.GetName(), err)
	case !ok:
		if expectedAdds != 0 || expectedDeletes != 0 {
			t.Errorf("no expectations found: expectedAdds %v, expectedDeletes %v", expectedAdds, expectedDeletes)
		}
	default:
		actualsAdds, actualDeletes := expectations.GetExpectations()
		if e, a := int64(expectedAdds), actualsAdds; e != a {
			t.Errorf("unexpected number of adds in expectations: expected %v, got %v", e, a)
		}
		if e, a := int64(expectedDeletes), actualDeletes; e != a {
			t.Errorf("unexpected number of deletes in expectations: expected %v, got %v", e, a)
		}
	}
}

// TestSyncClusterSteadyState tests syncing a cluster when it is in a steady
// state.
func TestSyncClusterSteadyState(t *testing.T) {
	cases := []struct {
		name     string
		computes []string
	}{
		{
			name: "only master",
		},
		{
			name:     "single compute",
			computes: []string{"compute1"},
		},
		{
			name:     "multiple computes",
			computes: []string{"compute1", "compute2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.computes...)
			cluster.Status.MasterNodeGroups = 1
			cluster.Status.ComputeNodeGroups = len(tc.computes)
			clusterStore.Add(cluster)
			newNodeGroups(nodeGroupStore, cluster, true, tc.computes...)

			controller.syncCluster(getKey(cluster, t))

			validateClientActions(t, clusterOperatorClient)

			validateControllerExpectations(t, controller, cluster, 0, 0)
		})
	}
}

// TestSyncClusterCreateNodeGroups tests syncing a cluster when node groups
// need to be created.
func TestSyncClusterCreateNodeGroups(t *testing.T) {
	cases := []struct {
		name             string
		existingMaster   bool
		existingComputes []string
		newComputes      []string
	}{
		{
			name:           "master",
			existingMaster: false,
		},
		{
			name:           "single compute",
			existingMaster: true,
			newComputes:    []string{"compute1"},
		},
		{
			name:           "multiple computes",
			existingMaster: true,
			newComputes:    []string{"compute1", "compute2"},
		},
		{
			name:           "master and computes",
			existingMaster: false,
			newComputes:    []string{"compute1", "compute2"},
		},
		{
			name:             "master with existing computes",
			existingMaster:   false,
			existingComputes: []string{"compute1", "compute2"},
		},
		{
			name:             "additional computes",
			existingMaster:   true,
			existingComputes: []string{"compute1", "compute2"},
			newComputes:      []string{"compute3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(append(tc.existingComputes, tc.newComputes...)...)
			if tc.existingMaster {
				cluster.Status.MasterNodeGroups = 1
			}
			cluster.Status.ComputeNodeGroups = len(tc.existingComputes)
			clusterStore.Add(cluster)
			newNodeGroups(nodeGroupStore, cluster, tc.existingMaster, tc.existingComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if !tc.existingMaster {
				expectedActions = append(expectedActions, newExpectedMasterNodeGroupCreateAction(cluster))
			}
			for _, newCompute := range tc.newComputes {
				expectedActions = append(expectedActions, newExpectedComputeNodeGroupCreateAction(cluster, newCompute))
			}

			validateClientActions(t, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, controller, cluster, len(expectedActions), 0)
		})
	}
}

// TestSyncClusterNodeGroupsAdded tests syncing a cluster when node groups
// have been created and need to be added to the status of the cluster.
func TestSyncClusterNodeGroupsAdded(t *testing.T) {
	cases := []struct {
		name        string
		masterAdded bool
		computes    []string
		oldComputes int
	}{
		{
			name:        "master",
			masterAdded: true,
		},
		{
			name:        "single compute",
			computes:    []string{"compute1"},
			oldComputes: 0,
		},
		{
			name:        "multiple computes",
			computes:    []string{"compute1", "compute2"},
			oldComputes: 0,
		},
		{
			name:        "master and  computes",
			masterAdded: true,
			computes:    []string{"compute1", "compute2"},
			oldComputes: 0,
		},
		{
			name:        "additional computes",
			computes:    []string{"compute1", "compute2", "compute3"},
			oldComputes: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.computes...)
			if !tc.masterAdded {
				cluster.Status.MasterNodeGroups = 1
			}
			cluster.Status.ComputeNodeGroups = tc.oldComputes
			clusterStore.Add(cluster)
			newNodeGroups(nodeGroupStore, cluster, true, tc.computes...)

			controller.syncCluster(getKey(cluster, t))

			validateClientActions(t, clusterOperatorClient,
				expectedClusterStatusUpdateAction{masterNodeGroups: 1, computeNodeGroups: len(tc.computes)},
			)

			validateControllerExpectations(t, controller, cluster, 0, 0)
		})
	}
}

// TestSyncClusterDeletedNodeGroups tests syncing a cluster when node groups
// in the cluster have been deleted.
func TestSyncClusterDeletedNodeGroups(t *testing.T) {
	cases := []struct {
		name             string
		masterDeleted    bool
		clusterComputes  []string
		realizedComputes []string
	}{
		{
			name:          "master",
			masterDeleted: true,
		},
		{
			name:            "single compute",
			clusterComputes: []string{"compute1"},
		},
		{
			name:            "multiple computes",
			clusterComputes: []string{"compute1", "compute2"},
		},
		{
			name:            "master and  computes",
			masterDeleted:   true,
			clusterComputes: []string{"compute1", "compute2"},
		},
		{
			name:             "subset of computes",
			clusterComputes:  []string{"compute1", "compute2", "compute3"},
			realizedComputes: []string{"compute1", "compute2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.clusterComputes...)
			cluster.Status.MasterNodeGroups = 1
			cluster.Status.ComputeNodeGroups = len(tc.clusterComputes)
			clusterStore.Add(cluster)
			newNodeGroups(nodeGroupStore, cluster, !tc.masterDeleted, tc.realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.masterDeleted {
				expectedActions = append(expectedActions, newExpectedMasterNodeGroupCreateAction(cluster))
			}
			for _, clusterCompute := range tc.clusterComputes {
				realized := false
				for _, realizedCompute := range tc.realizedComputes {
					if clusterCompute == realizedCompute {
						realized = true
						break
					}
				}
				if !realized {
					expectedActions = append(expectedActions, newExpectedComputeNodeGroupCreateAction(cluster, clusterCompute))
				}
			}
			statusMasterNodeGroups := 1
			if tc.masterDeleted {
				statusMasterNodeGroups = 0
			}
			expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{
				masterNodeGroups:  statusMasterNodeGroups,
				computeNodeGroups: len(tc.realizedComputes),
			})

			validateClientActions(t, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, controller, cluster, len(expectedActions)-1, 0)
		})
	}
}

// TestSyncClusterNodeGroupsRemoved tests syncing a cluster when node groups
// have been removed from the spec of the cluster.
func TestSyncClusterNodeGroupsRemoved(t *testing.T) {
	cases := []struct {
		name             string
		clusterComputes  []string
		realizedComputes []string
	}{
		{
			name:             "single compute",
			realizedComputes: []string{"compute1"},
		},
		{
			name:             "multiple computes",
			realizedComputes: []string{"compute1", "compute2"},
		},
		{
			name:             "subset of computes",
			clusterComputes:  []string{"compute1", "compute2"},
			realizedComputes: []string{"compute1", "compute2", "compute3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.clusterComputes...)
			cluster.Status.MasterNodeGroups = 1
			cluster.Status.ComputeNodeGroups = len(tc.clusterComputes)
			clusterStore.Add(cluster)
			newNodeGroups(nodeGroupStore, cluster, true, tc.realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			for _, realizedCompute := range tc.realizedComputes {
				includedInCluster := false
				for _, clusterCompute := range tc.clusterComputes {
					if realizedCompute == clusterCompute {
						includedInCluster = true
						break
					}
				}
				if !includedInCluster {
					expectedActions = append(expectedActions, newExpectedComputeNodeGroupDeleteAction(cluster, realizedCompute))
				}
			}

			validateClientActions(t, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, controller, cluster, 0, len(expectedActions))
		})
	}
}

// TestSyncClusterNodeGroupsMutated tests syncing a cluster when node group
// specifications in the cluster spec have been mutated.
func TestSyncClusterNodeGroupsMutated(t *testing.T) {
	cases := []struct {
		name                 string
		clusterMasterSize    int
		realizedMasterSize   int
		clusterComputeSizes  []int
		realizedComputeSizes []int
	}{
		{
			name:               "master only",
			clusterMasterSize:  1,
			realizedMasterSize: 2,
		},
		{
			name:                 "single compute",
			clusterMasterSize:    1,
			realizedMasterSize:   1,
			clusterComputeSizes:  []int{1},
			realizedComputeSizes: []int{2},
		},
		{
			name:                 "multiple computes",
			clusterMasterSize:    1,
			realizedMasterSize:   1,
			clusterComputeSizes:  []int{1, 2},
			realizedComputeSizes: []int{2, 1},
		},
		{
			name:                 "master and computes",
			clusterMasterSize:    1,
			realizedMasterSize:   2,
			clusterComputeSizes:  []int{1, 2},
			realizedComputeSizes: []int{2, 1},
		},
		{
			name:                 "subset of computes",
			clusterMasterSize:    1,
			realizedMasterSize:   2,
			clusterComputeSizes:  []int{1, 2, 3},
			realizedComputeSizes: []int{2, 1, 3},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if a, b := len(tc.clusterComputeSizes), len(tc.realizedComputeSizes); a != b {
				t.Skipf("clusterComputeSizes length must be equal to realizedComputeSizes length: %v, %v", a, b)
			}

			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			clusterComputes := make([]clusteroperator.ClusterComputeNodeGroup, len(tc.clusterComputeSizes))
			realizedComputes := make([]clusteroperator.ClusterComputeNodeGroup, len(tc.realizedComputeSizes))
			for i := range tc.clusterComputeSizes {
				name := fmt.Sprintf("compute%v", i)
				clusterComputes[i] = clusteroperator.ClusterComputeNodeGroup{
					ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
						Size: tc.clusterComputeSizes[i],
					},
					Name: name,
				}
				realizedComputes[i] = clusteroperator.ClusterComputeNodeGroup{
					ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
						Size: tc.realizedComputeSizes[i],
					},
					Name: name,
				}
			}
			cluster := newClusterWithSizes(tc.clusterMasterSize, clusterComputes...)
			cluster.Status.MasterNodeGroups = 1
			cluster.Status.ComputeNodeGroups = len(clusterComputes)
			clusterStore.Add(cluster)
			newNodeGroupsWithSizes(nodeGroupStore, cluster, tc.realizedMasterSize, realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.clusterMasterSize != tc.realizedMasterSize {
				expectedActions = append(expectedActions,
					newExpectedMasterNodeGroupDeleteAction(cluster),
					newExpectedMasterNodeGroupCreateAction(cluster),
				)
			}
			for i, clusterCompute := range clusterComputes {
				if clusterCompute.Size == tc.realizedComputeSizes[i] {
					continue
				}
				expectedActions = append(expectedActions,
					newExpectedComputeNodeGroupDeleteAction(cluster, clusterCompute.Name),
					newExpectedComputeNodeGroupCreateAction(cluster, clusterCompute.Name),
				)
			}

			validateClientActions(t, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, controller, cluster, len(expectedActions)/2, len(expectedActions)/2)
		})
	}
}

// TestSyncClusterNodeGroupOwnerReference tests syncing a cluster when there
// are node groups that belong to other clusters.
func TestSyncClusterNodeGroupOwnerReference(t *testing.T) {
	trueVar := true
	cases := []struct {
		name            string
		ownerRef        *metav1.OwnerReference
		expectNewMaster bool
	}{
		{
			name: "owned",
			ownerRef: &metav1.OwnerReference{
				UID:        testClusterUUID,
				APIVersion: clusteroperator.SchemeGroupVersion.String(),
				Kind:       "Cluster",
				Controller: &trueVar,
			},
			expectNewMaster: false,
		},
		{
			name:            "no owner",
			expectNewMaster: true,
		},
		{
			name: "different owner",
			ownerRef: &metav1.OwnerReference{
				UID:        types.UID("other-cluster-uuid"),
				APIVersion: clusteroperator.SchemeGroupVersion.String(),
				Kind:       "Cluster",
				Controller: &trueVar,
			},
			expectNewMaster: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster()
			clusterStore.Add(cluster)

			nodeGroupName := fmt.Sprintf("%s-master-random", cluster.Name)
			nodeGroup := newNodeGroup(nodeGroupName, cluster, false)
			nodeGroup.Spec.NodeType = clusteroperator.NodeTypeMaster
			nodeGroup.Spec.Size = 1
			if tc.ownerRef != nil {
				nodeGroup.OwnerReferences = []metav1.OwnerReference{*tc.ownerRef}
			}
			nodeGroupStore.Add(nodeGroup)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.expectNewMaster {
				expectedActions = append(expectedActions, newExpectedMasterNodeGroupCreateAction(cluster))
			} else {
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{masterNodeGroups: 1})
			}

			validateClientActions(t, clusterOperatorClient, expectedActions...)

			expectedAdds := 0
			if tc.expectNewMaster {
				expectedAdds = 1
			}
			validateControllerExpectations(t, controller, cluster, expectedAdds, 0)
		})
	}
}

// TestSyncClusterNodeGroupDeletionTimestamp tests syncing a cluster when
// a node group has a non-nil deletion timestamp.
func TestSyncClusterNodeGroupDeletionTimestamp(t *testing.T) {
	cases := []struct {
		name              string
		deletionTimestamp *metav1.Time
		expectNewMaster   bool
	}{
		{
			name:              "not deleted",
			deletionTimestamp: nil,
			expectNewMaster:   false,
		},
		{
			name:              "deleted",
			deletionTimestamp: &metav1.Time{},
			expectNewMaster:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster()
			clusterStore.Add(cluster)

			nodeGroupName := fmt.Sprintf("%s-master-random", cluster.Name)
			nodeGroup := newNodeGroup(nodeGroupName, cluster, true)
			nodeGroup.Spec.NodeType = clusteroperator.NodeTypeMaster
			nodeGroup.Spec.Size = 1
			nodeGroup.DeletionTimestamp = tc.deletionTimestamp
			nodeGroupStore.Add(nodeGroup)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.expectNewMaster {
				expectedActions = append(expectedActions, newExpectedMasterNodeGroupCreateAction(cluster))
			} else {
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{masterNodeGroups: 1})
			}

			validateClientActions(t, clusterOperatorClient, expectedActions...)

			expectedAdds := 0
			if tc.expectNewMaster {
				expectedAdds = 1
			}
			validateControllerExpectations(t, controller, cluster, expectedAdds, 0)
		})
	}
}

// TestSyncClusterComplex tests syncing a cluster when there are numerous
// changes to the cluster spec and the node groups.
func TestSyncClusterComplex(t *testing.T) {
	controller, clusterStore, nodeGroupStore, _, clusterOperatorClient := newTestClusterController()

	cluster := newClusterWithSizes(1,
		clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
			Name: "realized and un-mutated",
		},
		clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
			Name: "realized but mutated",
		},
		clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
			Name: "unrealized",
		},
	)
	clusterStore.Add(cluster)

	newNodeGroupsWithSizes(nodeGroupStore, cluster, 2,
		clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
			Name: "realized and un-mutated",
		},
		clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 2,
			},
			Name: "realized but mutated",
		},
		clusteroperator.ClusterComputeNodeGroup{
			ClusterNodeGroup: clusteroperator.ClusterNodeGroup{
				Size: 1,
			},
			Name: "removed from cluster",
		},
	)

	controller.syncCluster(getKey(cluster, t))

	validateClientActions(t, clusterOperatorClient,
		newExpectedMasterNodeGroupDeleteAction(cluster),
		newExpectedMasterNodeGroupCreateAction(cluster),
		newExpectedComputeNodeGroupDeleteAction(cluster, "realized but mutated"),
		newExpectedComputeNodeGroupCreateAction(cluster, "realized but mutated"),
		newExpectedComputeNodeGroupCreateAction(cluster, "unrealized"),
		newExpectedComputeNodeGroupDeleteAction(cluster, "removed from cluster"),
		expectedClusterStatusUpdateAction{masterNodeGroups: 1, computeNodeGroups: 2}, // status only counts the 2 realized compute nodes
	)

	validateControllerExpectations(t, controller, cluster, 3, 3)
}
