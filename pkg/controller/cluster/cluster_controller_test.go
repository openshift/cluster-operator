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
	"encoding/json"
	"fmt"
	"reflect"
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

var (
	defaultClusterVersion = clusteroperator.ClusterVersionReference{Name: "v3-9"}
)

// newTestClusterController creates a test ClusterController with fake
// clients and informers.
func newTestClusterController() (
	*ClusterController,
	cache.Store, // cluster store
	cache.Store, // machine set store
	*clientgofake.Clientset,
	*clusteroperatorclientset.Clientset,
) {
	kubeClient := &clientgofake.Clientset{}
	clusterOperatorClient := &clusteroperatorclientset.Clientset{}
	informers := informers.NewSharedInformerFactory(clusterOperatorClient, 0)

	controller := NewClusterController(
		informers.Clusteroperator().V1alpha1().Clusters(),
		informers.Clusteroperator().V1alpha1().MachineSets(),
		kubeClient,
		clusterOperatorClient,
	)

	controller.clustersSynced = alwaysReady
	controller.machineSetsSynced = alwaysReady

	return controller,
		informers.Clusteroperator().V1alpha1().Clusters().Informer().GetStore(),
		informers.Clusteroperator().V1alpha1().MachineSets().Informer().GetStore(),
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

// newCluster creates a new cluster with compute machine sets that have
// the specified names.
func newCluster(computeNames ...string) *clusteroperator.Cluster {
	computes := make([]clusteroperator.ClusterMachineSet, len(computeNames))
	for i, computeName := range computeNames {
		computes[i] = clusteroperator.ClusterMachineSet{
			Name: computeName,
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
			},
		}
	}
	return newClusterWithSizes(1, computes...)
}

// newClusterWithSizes creates a new cluster with the specified size for the
// master machine set and the specified cluster compute machine sets.
func newClusterWithSizes(masterSize int, computes ...clusteroperator.ClusterMachineSet) *clusteroperator.Cluster {
	cluster := &clusteroperator.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterUUID,
			Name:      testClusterName,
			Namespace: testNamespace,
		},
		Spec: clusteroperator.ClusterSpec{
			MachineSets: append(computes, clusteroperator.ClusterMachineSet{
				Name: "master",
				MachineSetConfig: clusteroperator.MachineSetConfig{
					Infra:    true,
					Size:     masterSize,
					NodeType: clusteroperator.NodeTypeMaster,
				},
			}),
			Version: defaultClusterVersion,
		},
		Status: clusteroperator.ClusterStatus{
			MasterMachineSetName: testClusterName + "-master-random",
			InfraMachineSetName:  testClusterName + "-master-random",
		},
	}
	return cluster
}

// newMachineSet creates a new machine set.
func newMachineSet(name string, cluster *clusteroperator.Cluster, properlyOwned bool) *clusteroperator.MachineSet {
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
	return &clusteroperator.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{controllerReference},
		},
	}
}

// newMachineSets creates new machine sets and stores them in the specified
// store.
func newMachineSets(store cache.Store, cluster *clusteroperator.Cluster, includeMaster bool, computeNames ...string) []*clusteroperator.MachineSet {
	masterSize := 0
	if includeMaster {
		masterSize = 1
	}

	computes := make([]clusteroperator.ClusterMachineSet, len(computeNames))
	for i, computeName := range computeNames {
		computes[i] = clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				NodeType: clusteroperator.NodeTypeCompute,
				Size:     1,
			},
			Name: computeName,
		}
	}

	return newMachineSetsWithSizes(store, cluster, masterSize, computes...)
}

// newMachineSetsWithSizes creates new machine sets, with specific sizes, and
// stores them in the specified store.
func newMachineSetsWithSizes(store cache.Store, cluster *clusteroperator.Cluster, masterSize int, computes ...clusteroperator.ClusterMachineSet) []*clusteroperator.MachineSet {
	machineSets := []*clusteroperator.MachineSet{}
	if masterSize > 0 {
		name := fmt.Sprintf("%s-master-random", cluster.Name)
		machineSet := newMachineSet(name, cluster, true)
		machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
		machineSet.Spec.Size = masterSize
		machineSet.Spec.Infra = true
		machineSet.Spec.Version = cluster.Spec.Version
		machineSets = append(machineSets, machineSet)
	}
	for _, compute := range computes {
		name := fmt.Sprintf("%s-%s-random", cluster.Name, compute.Name)
		machineSet := newMachineSet(name, cluster, true)
		machineSet.Spec.MachineSetConfig = compute.MachineSetConfig
		machineSet.Spec.Version = cluster.Spec.Version
		machineSets = append(machineSets, machineSet)
	}
	if store != nil {
		for _, machineSet := range machineSets {
			store.Add(machineSet)
		}
	}
	return machineSets

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
func validateClientActions(t *testing.T, testName string, clusterOperatorClient *clusteroperatorclientset.Clientset, expectedActions ...expectedClientAction) {
	actualActions := clusterOperatorClient.Actions()
	if e, a := len(expectedActions), len(actualActions); e != a {
		t.Errorf("%s: unexpected number of client actions: expected %v, got %v", testName, e, a)
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
			t.Errorf("%s: unexpected client action: %+v", testName, actualAction)
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

// expectedMachineSetCreateAction is an expected client action to create a
// machine set.
type expectedMachineSetCreateAction struct {
	namePrefix string
}

func (ea expectedMachineSetCreateAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("machinesets")
}

func (ea expectedMachineSetCreateAction) verb() string {
	return "create"
}

func (ea expectedMachineSetCreateAction) validate(t *testing.T, action clientgotesting.Action) bool {
	createAction, ok := action.(clientgotesting.CreateAction)
	if !ok {
		t.Errorf("create action is not a CreateAction: %t", createAction)
		return false
	}
	createdObject := createAction.GetObject()
	machineSet, ok := createdObject.(*clusteroperator.MachineSet)
	if !ok {
		t.Errorf("machine set create action object is not a MachineSet: %t", machineSet)
		return false
	}
	return machineSet.GenerateName == ea.namePrefix
}

// newExpectedMachineSetCreateAction creates a new expected client
// action for creating a compute machine set.
func newExpectedMachineSetCreateAction(cluster *clusteroperator.Cluster, name string) expectedMachineSetCreateAction {
	return expectedMachineSetCreateAction{
		namePrefix: getNamePrefixForMachineSet(cluster, name),
	}
}

// expectedMachineSetDeleteAction is an expected client action to delete a
// machine set.
type expectedMachineSetDeleteAction struct {
	namePrefix string
}

func (ea expectedMachineSetDeleteAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("machinesets")
}

func (ea expectedMachineSetDeleteAction) verb() string {
	return "delete"
}

func (ea expectedMachineSetDeleteAction) validate(t *testing.T, action clientgotesting.Action) bool {
	deleteAction, ok := action.(clientgotesting.DeleteAction)
	if !ok {
		t.Errorf("delete action is not a DeleteAction: %t", deleteAction)
		return false
	}
	return strings.HasPrefix(deleteAction.GetName(), ea.namePrefix)
}

// newExpectedMachineSetDeleteAction creates a new expected client
// action for deleting a machine set.
func newExpectedMachineSetDeleteAction(cluster *clusteroperator.Cluster, name string) expectedMachineSetDeleteAction {
	return expectedMachineSetDeleteAction{
		namePrefix: getNamePrefixForMachineSet(cluster, name),
	}
}

// expectedClusterStatusUpdateAction is an expected client action to update
// the status of a cluster.
type expectedClusterStatusUpdateAction struct {
	machineSets int
}

func (ea expectedClusterStatusUpdateAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("clusters")
}

func (ea expectedClusterStatusUpdateAction) verb() string {
	return "patch"
}

func (ea expectedClusterStatusUpdateAction) validate(t *testing.T, action clientgotesting.Action) bool {
	if action.GetSubresource() != "status" {
		return false
	}
	updateAction, ok := action.(clientgotesting.PatchAction)
	if !ok {
		t.Errorf("update action is not an UpdateAction: %t", updateAction)
		return false
	}
	patch := map[string]interface{}{}
	if err := json.Unmarshal(updateAction.GetPatch(), &patch); err != nil {
		t.Errorf("cannot unmarshal patch: %v. %v", string(updateAction.GetPatch()), err)
		return true
	}
	if e, a := 1, len(patch); e != a {
		t.Errorf("unexpected number of fields in patch: expected %v, got %v", e, a)
		return true
	}
	statusAsInterface := patch["status"]
	if statusAsInterface == nil {
		t.Errorf("expected status field in patch")
		return true
	}
	status, ok := statusAsInterface.(map[string]interface{})
	if !ok {
		t.Errorf("status field is not an object")
		return true
	}
	if e, a := 1, len(status); e != a {
		t.Errorf("unexpected number of fields in status field; expected %v, got %v", e, a)
		return true
	}
	machineSetCountAsInterface := status["machineSetCount"]
	if machineSetCountAsInterface == nil {
		t.Errorf("expected status.machineSetCount field in patch")
		return true
	}
	machineSetCount, ok := machineSetCountAsInterface.(float64)
	if !ok {
		t.Errorf("status.machineSetCount is not an int: %T", machineSetCountAsInterface)
		return true
	}
	if e, a := ea.machineSets, int(machineSetCount); e != a {
		t.Errorf("unexpected machineSets in cluster update status: expected %v, got %v", e, a)
		return true
	}
	return true
}

// validateControllerExpectations validates that the specified cluster is
// expecting the specified number of creations and deletions of machine sets.
func validateControllerExpectations(t *testing.T, testName string, ctrlr *ClusterController, cluster *clusteroperator.Cluster, expectedAdds, expectedDeletes int) {
	expectations, ok, err := ctrlr.expectations.GetExpectations(getKey(cluster, t))
	switch {
	case err != nil:
		t.Errorf("%s: error getting expecations: %v", testName, cluster.GetName(), err)
	case !ok:
		if expectedAdds != 0 || expectedDeletes != 0 {
			t.Errorf("%s: no expectations found: expectedAdds %v, expectedDeletes %v", testName, expectedAdds, expectedDeletes)
		}
	default:
		actualsAdds, actualDeletes := expectations.GetExpectations()
		if e, a := int64(expectedAdds), actualsAdds; e != a {
			t.Errorf("%s: unexpected number of adds in expectations: expected %v, got %v", testName, e, a)
		}
		if e, a := int64(expectedDeletes), actualDeletes; e != a {
			t.Errorf("%s: unexpected number of deletes in expectations: expected %v, got %v", testName, e, a)
		}
	}
}

// TestApplyDefaultMachineSetHardwareSpec tests merging a default hardware spec with a specific spec from a
// machine set
func TestApplyDefaultMachineSetHardwareSpec(t *testing.T) {

	awsSpec := func(amiName, instanceType string) *clusteroperator.MachineSetHardwareSpec {
		return &clusteroperator.MachineSetHardwareSpec{
			AWS: &clusteroperator.MachineSetAWSHardwareSpec{
				AMIName:      amiName,
				InstanceType: instanceType,
			},
		}
	}
	cases := []struct {
		name        string
		defaultSpec *clusteroperator.MachineSetHardwareSpec
		specific    *clusteroperator.MachineSetHardwareSpec
		expected    *clusteroperator.MachineSetHardwareSpec
	}{
		{
			name:        "no default",
			defaultSpec: nil,
			specific:    awsSpec("base-ami", "large-instance"),
			expected:    awsSpec("base-ami", "large-instance"),
		},
		{
			name:        "only default",
			defaultSpec: awsSpec("base-ami", "small-instance"),
			specific:    &clusteroperator.MachineSetHardwareSpec{},
			expected:    awsSpec("base-ami", "small-instance"),
		},
		{
			name:        "override default",
			defaultSpec: awsSpec("base-ami", "large-instance"),
			specific:    awsSpec("", "specific-instance"),
			expected:    awsSpec("base-ami", "specific-instance"),
		},
		{
			name:        "partial default",
			defaultSpec: awsSpec("base-ami", ""),
			specific:    awsSpec("", "large-instance"),
			expected:    awsSpec("base-ami", "large-instance"),
		},
	}

	for _, tc := range cases {
		result, err := applyDefaultMachineSetHardwareSpec(tc.specific, tc.defaultSpec)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(result, tc.expected) {
			t.Errorf("%s: unexpected result. Expected: %v, Got: %v", tc.name, tc.expected, result)
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.computes...)
			cluster.Status.MachineSetCount = len(tc.computes) + 1
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, true, tc.computes...)

			controller.syncCluster(getKey(cluster, t))

			validateClientActions(t, "TestSyncClusterSteadyState."+tc.name, clusterOperatorClient)

			validateControllerExpectations(t, "TestSyncClusterSteadyState."+tc.name, controller, cluster, 0, 0)
		})
	}
}

// TestSyncClusterCreateMachineSets tests syncing a cluster when machine sets
// need to be created.
func TestSyncClusterCreateMachineSets(t *testing.T) {
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(append(tc.existingComputes, tc.newComputes...)...)
			cluster.Status.MachineSetCount = len(tc.existingComputes) + 1
			if !tc.existingMaster {
				cluster.Status.MachineSetCount--
			}
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, tc.existingMaster, tc.existingComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if !tc.existingMaster {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, "master"))
			}
			for _, newCompute := range tc.newComputes {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, newCompute))
			}

			validateClientActions(t, "TestSyncClusterCreateMachineSets."+tc.name, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, "TestSyncClusterCreateMachineSets."+tc.name, controller, cluster, len(expectedActions), 0)
		})
	}
}

// TestSyncClusterMachineSetsAdded tests syncing a cluster when machine sets
// have been created and need to be added to the status of the cluster.
func TestSyncClusterMachineSetsAdded(t *testing.T) {
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.computes...)
			cluster.Status.MachineSetCount = tc.oldComputes
			if !tc.masterAdded {
				cluster.Status.MachineSetCount++
			}
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, true, tc.computes...)

			controller.syncCluster(getKey(cluster, t))

			validateClientActions(t, "TestSyncClusterMachineSetsAdded."+tc.name, clusterOperatorClient,
				expectedClusterStatusUpdateAction{machineSets: 1 + len(tc.computes)},
			)
			validateControllerExpectations(t, "TestSyncClusterMachineSetsAdded."+tc.name, controller, cluster, 0, 0)
		})
	}
}

// TestSyncClusterDeletedMachineSets tests syncing a cluster when machine sets
// in the cluster have been deleted.
func TestSyncClusterDeletedMachineSets(t *testing.T) {
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.clusterComputes...)
			cluster.Status.MachineSetCount = len(tc.clusterComputes) + 1
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, !tc.masterDeleted, tc.realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.masterDeleted {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, "master"))
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
					expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, clusterCompute))
				}
			}
			statusMasterMachineSets := 1
			if tc.masterDeleted {
				statusMasterMachineSets = 0
			}
			expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{
				machineSets: len(tc.realizedComputes) + statusMasterMachineSets,
			})

			validateClientActions(t, "TestSyncClusterDeletedMachineSets."+tc.name, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, "TestSyncClusterDeletedMachineSets."+tc.name, controller, cluster, len(expectedActions)-1, 0)
		})
	}
}

// TestSyncClusterMachineSetsRemoved tests syncing a cluster when machine sets
// have been removed from the spec of the cluster.
func TestSyncClusterMachineSetsRemoved(t *testing.T) {
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster(tc.clusterComputes...)
			cluster.Status.MachineSetCount = len(tc.clusterComputes) + 1
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, true, tc.realizedComputes...)

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
					expectedActions = append(expectedActions, newExpectedMachineSetDeleteAction(cluster, realizedCompute))
				}
			}

			validateClientActions(t, "TestSyncClusterMachineSetsRemoved."+tc.name, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, "TestSyncClusterMachineSetsRemoved."+tc.name, controller, cluster, 0, len(expectedActions))
		})
	}
}

// TestSyncClusterMachineSetSpecMutated tests syncing a cluster when machine set
// specifications in the cluster spec have been mutated.
func TestSyncClusterMachineSetSpecMutated(t *testing.T) {
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

			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			clusterComputes := make([]clusteroperator.ClusterMachineSet, len(tc.clusterComputeSizes))
			realizedComputes := make([]clusteroperator.ClusterMachineSet, len(tc.realizedComputeSizes))
			for i := range tc.clusterComputeSizes {
				name := fmt.Sprintf("compute%v", i)
				clusterComputes[i] = clusteroperator.ClusterMachineSet{
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: clusteroperator.NodeTypeCompute,
						Size:     tc.clusterComputeSizes[i],
					},
					Name: name,
				}
				realizedComputes[i] = clusteroperator.ClusterMachineSet{
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: clusteroperator.NodeTypeCompute,
						Size:     tc.realizedComputeSizes[i],
					},
					Name: name,
				}
			}
			cluster := newClusterWithSizes(tc.clusterMasterSize, clusterComputes...)
			cluster.Status.MachineSetCount = len(clusterComputes) + 1
			clusterStore.Add(cluster)
			newMachineSetsWithSizes(machineSetStore, cluster, tc.realizedMasterSize, realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.clusterMasterSize != tc.realizedMasterSize {
				expectedActions = append(expectedActions,
					newExpectedMachineSetDeleteAction(cluster, "master"),
					newExpectedMachineSetCreateAction(cluster, "master"),
				)
			}
			for i, clusterCompute := range clusterComputes {
				if clusterCompute.Size == tc.realizedComputeSizes[i] {
					continue
				}
				expectedActions = append(expectedActions,
					newExpectedMachineSetDeleteAction(cluster, clusterCompute.Name),
					newExpectedMachineSetCreateAction(cluster, clusterCompute.Name),
				)
			}

			validateClientActions(t, "TestSyncClusterMachineSetsMutated."+tc.name, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, "TestSyncClusterMachineSetsMutated."+tc.name, controller, cluster, len(expectedActions)/2, len(expectedActions)/2)
		})
	}
}

//  TestSyncClusterVersionMutated tests that changing a cluster spec version results in new machine sets being created with the new version.
func TestSyncClusterVersionMutated(t *testing.T) {
	controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

	// A different cluster version to the default:
	clusterVer310 := clusteroperator.ClusterVersionReference{
		Name: "v3-10",
	}

	// Create compute machine set
	name := "compute0"
	clusterCompute := clusteroperator.ClusterMachineSet{
		MachineSetConfig: clusteroperator.MachineSetConfig{
			NodeType: clusteroperator.NodeTypeCompute,
			Size:     5,
		},
		Name: name,
	}
	realizedCompute := clusteroperator.ClusterMachineSet{
		MachineSetConfig: clusteroperator.MachineSetConfig{
			NodeType: clusteroperator.NodeTypeCompute,
			Size:     5,
		},
		Name: name,
	}

	// Create the new incoming cluster state, now referencing 3.10:
	cluster := newClusterWithSizes(3, clusterCompute)
	cluster.Status.MachineSetCount = 2
	clusterStore.Add(cluster)

	// Simulate the pre-existing MachineSet state:
	newMachineSetsWithSizes(machineSetStore, cluster, 3, realizedCompute)

	// Sync and ensure no expected actions as we haven't changed the version yet:
	controller.syncCluster(getKey(cluster, t))
	validateClientActions(t, "TestSyncClusterVersionMutated", clusterOperatorClient, []expectedClientAction{}...)
	validateControllerExpectations(t, "TestSyncClusterVersionMutated", controller, cluster, 0, 0)

	// Changing the cluster version to 3.10 should result in machine sets being recreated:
	cluster.Spec.Version = clusterVer310

	controller.syncCluster(getKey(cluster, t))

	expectedActions := []expectedClientAction{}
	expectedActions = append(expectedActions,
		newExpectedMachineSetDeleteAction(cluster, "master"),
		newExpectedMachineSetCreateAction(cluster, "master"),
	)

	expectedActions = append(expectedActions,
		newExpectedMachineSetDeleteAction(cluster, clusterCompute.Name),
		newExpectedMachineSetCreateAction(cluster, clusterCompute.Name),
	)

	validateClientActions(t, "TestSyncClusterVersionMutated", clusterOperatorClient, expectedActions...)

	validateControllerExpectations(t, "TestSyncClusterVersionMutated", controller, cluster, len(expectedActions)/2, len(expectedActions)/2)
}

// TestSyncClusterMachineSetOwnerReference tests syncing a cluster when there
// are machine sets that belong to other clusters.
func TestSyncClusterMachineSetOwnerReference(t *testing.T) {
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster()
			clusterStore.Add(cluster)

			machineSetName := fmt.Sprintf("%s-master-random", cluster.Name)
			machineSet := newMachineSet(machineSetName, cluster, false)
			machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
			machineSet.Spec.Size = 1
			machineSet.Spec.Infra = true
			machineSet.Spec.Version = defaultClusterVersion
			if tc.ownerRef != nil {
				machineSet.OwnerReferences = []metav1.OwnerReference{*tc.ownerRef}
			}
			machineSetStore.Add(machineSet)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.expectNewMaster {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, "master"))
			} else {
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{machineSets: 1})
			}

			validateClientActions(t, "TestSyncClusterMachineSetOwnerReference."+tc.name, clusterOperatorClient, expectedActions...)

			expectedAdds := 0
			if tc.expectNewMaster {
				expectedAdds = 1
			}
			validateControllerExpectations(t, "TestSyncClusterMachineSetOwnerReference."+tc.name, controller, cluster, expectedAdds, 0)
		})
	}
}

// TestSyncClusterMachineSetDeletionTimestamp tests syncing a cluster when
// a machine set has a non-nil deletion timestamp.
func TestSyncClusterMachineSetDeletionTimestamp(t *testing.T) {
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
			controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

			cluster := newCluster()
			clusterStore.Add(cluster)

			machineSetName := fmt.Sprintf("%s-master-random", cluster.Name)
			machineSet := newMachineSet(machineSetName, cluster, true)
			machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
			machineSet.Spec.Size = 1
			machineSet.Spec.Infra = true
			machineSet.Spec.Version = defaultClusterVersion
			machineSet.DeletionTimestamp = tc.deletionTimestamp
			machineSetStore.Add(machineSet)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.expectNewMaster {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, "master"))
			} else {
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{machineSets: 1})
			}

			validateClientActions(t, "TestSyncClusterMachineSetDeletionTimestamp."+tc.name, clusterOperatorClient, expectedActions...)

			expectedAdds := 0
			if tc.expectNewMaster {
				expectedAdds = 1
			}
			validateControllerExpectations(t, "TestSyncClusterMachineSetDeletionTimestamp."+tc.name, controller, cluster, expectedAdds, 0)
		})
	}
}

// TestSyncClusterComplex tests syncing a cluster when there are numerous
// changes to the cluster spec and the machine sets.
func TestSyncClusterComplex(t *testing.T) {
	controller, clusterStore, machineSetStore, _, clusterOperatorClient := newTestClusterController()

	cluster := newClusterWithSizes(1,
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
			},
			Name: "realized and un-mutated",
		},
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
			},
			Name: "realized but mutated",
		},
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
			},
			Name: "unrealized",
		},
	)
	clusterStore.Add(cluster)

	newMachineSetsWithSizes(machineSetStore, cluster, 2,
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
			},
			Name: "realized and un-mutated",
		},
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     2,
				NodeType: clusteroperator.NodeTypeCompute,
			},
			Name: "realized but mutated",
		},
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
			},
			Name: "removed from cluster",
		},
	)

	controller.syncCluster(getKey(cluster, t))

	validateClientActions(t, "TestSyncClusterComplex", clusterOperatorClient,
		newExpectedMachineSetDeleteAction(cluster, "master"),
		newExpectedMachineSetCreateAction(cluster, "master"),
		newExpectedMachineSetDeleteAction(cluster, "realized but mutated"),
		newExpectedMachineSetCreateAction(cluster, "realized but mutated"),
		newExpectedMachineSetCreateAction(cluster, "unrealized"),
		newExpectedMachineSetDeleteAction(cluster, "removed from cluster"),
		expectedClusterStatusUpdateAction{machineSets: 3}, // status only counts the 2 realized compute nodes + 1 master
	)

	validateControllerExpectations(t, "TestSyncClusterComplex", controller, cluster, 3, 3)
}
