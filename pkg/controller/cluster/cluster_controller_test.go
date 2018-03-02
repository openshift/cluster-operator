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

	corev1 "k8s.io/api/core/v1"
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

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	testNamespace      = "test-namespace"
	testClusterName    = "test-cluster"
	testClusterUUID    = types.UID("test-cluster-uuid")
	testClusterVerName = "v3-9"
	testClusterVerNS   = "cluster-operator"
	testClusterVerUID  = types.UID("test-cluster-version")
	testRegion         = "us-east-1"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

// newTestController creates a test Controller with fake
// clients and informers.
func newTestController() (
	*Controller,
	cache.Store, // cluster store
	cache.Store, // machine set store
	cache.Store, // cluster version store
	*clientgofake.Clientset,
	*clusteroperatorclientset.Clientset,
) {
	kubeClient := &clientgofake.Clientset{}
	clusterOperatorClient := &clusteroperatorclientset.Clientset{}
	informers := informers.NewSharedInformerFactory(clusterOperatorClient, 0)

	controller := NewController(
		informers.Clusteroperator().V1alpha1().Clusters(),
		informers.Clusteroperator().V1alpha1().MachineSets(),
		informers.Clusteroperator().V1alpha1().ClusterVersions(),
		kubeClient,
		clusterOperatorClient,
	)

	controller.clustersSynced = alwaysReady
	controller.machineSetsSynced = alwaysReady

	return controller,
		informers.Clusteroperator().V1alpha1().Clusters().Informer().GetStore(),
		informers.Clusteroperator().V1alpha1().MachineSets().Informer().GetStore(),
		informers.Clusteroperator().V1alpha1().ClusterVersions().Informer().GetStore(),
		kubeClient,
		clusterOperatorClient
}

// alwaysReady is a function that can be used as a sync function that will
// always indicate that the lister has been synced.
var alwaysReady = func() bool { return true }

// getKey gets the key for the cluster to use when checking expectations
// set on a cluster.
func getKey(cluster *clusteroperator.Cluster, t *testing.T) string {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		t.Errorf("Unexpected error getting key for Cluster %v: %v", cluster.Name, err)
		return ""
	}
	return key
}

// newCluster creates a new cluster with compute machine sets that have
// the specified names.
func newCluster(cv *clusteroperator.ClusterVersion, computeNames ...string) *clusteroperator.Cluster {
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
	return newClusterWithSizes(cv, 1, computes...)
}

// newClusterWithMasterInstanceType creates a new cluster with the specified instance type for the
// master machine set and the specified cluster compute machine sets. If a clusterversion is
// provided we set the spec version ref, as well as the status version ref. This implies
// that the caller has added the provided version to the store.
func newClusterWithMasterInstanceType(cv *clusteroperator.ClusterVersion, instanceType string, computes ...clusteroperator.ClusterMachineSet) *clusteroperator.Cluster {
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
					Size:     1,
					Infra:    true,
					NodeType: clusteroperator.NodeTypeMaster,
					Hardware: &clusteroperator.MachineSetHardwareSpec{
						AWS: &clusteroperator.MachineSetAWSHardwareSpec{
							InstanceType: instanceType,
						},
					},
				},
			}),
			Hardware: clusteroperator.ClusterHardwareSpec{
				AWS: &clusteroperator.AWSClusterSpec{
					Region: testRegion,
				},
			},
		},
		Status: clusteroperator.ClusterStatus{
			MasterMachineSetName: testClusterName + "-master-random",
			InfraMachineSetName:  testClusterName + "-master-random",
		},
	}

	// If clusterversion was provided we assume it exists in the store and set both
	// the spec and the status:
	if cv != nil {
		cluster.Spec.ClusterVersionRef = clusteroperator.ClusterVersionReference{
			Name:      cv.Name,
			Namespace: cv.Namespace,
		}
		cluster.Status.ClusterVersionRef = &corev1.ObjectReference{
			Name:      cv.Name,
			Namespace: cv.Namespace,
			UID:       cv.UID,
		}
	}
	return cluster
}

// newClusterWithSizes creates a new cluster with the specified size for the
// master machine set and the specified cluster compute machine sets. If a clusterversion is
// provided we set the spec version ref, as well as the status version ref. This implies
// that the caller has added the provided version to the store.
func newClusterWithSizes(cv *clusteroperator.ClusterVersion, masterSize int, computes ...clusteroperator.ClusterMachineSet) *clusteroperator.Cluster {
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
			Hardware: clusteroperator.ClusterHardwareSpec{
				AWS: &clusteroperator.AWSClusterSpec{
					Region: testRegion,
				},
			},
		},
		Status: clusteroperator.ClusterStatus{
			MasterMachineSetName: testClusterName + "-master-random",
			InfraMachineSetName:  testClusterName + "-master-random",
		},
	}
	// If clusterversion was provided we assume it exists in the store and set both
	// the spec and the status:
	if cv != nil {
		cluster.Spec.ClusterVersionRef = clusteroperator.ClusterVersionReference{
			Name:      cv.Name,
			Namespace: cv.Namespace,
		}
		cluster.Status.ClusterVersionRef = &corev1.ObjectReference{
			Name:      cv.Name,
			Namespace: cv.Namespace,
			UID:       cv.UID,
		}
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
func newMachineSets(store cache.Store, cluster *clusteroperator.Cluster, clusterVersion *clusteroperator.ClusterVersion, includeMaster bool, computeNames ...string) []*clusteroperator.MachineSet {
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

	return newMachineSetsWithSizes(store, cluster, clusterVersion, masterSize, computes...)
}

// newMachineSetsWithSizes creates new machine sets, with specific sizes, and
// stores them in the specified store.
func newMachineSetsWithSizes(store cache.Store, cluster *clusteroperator.Cluster, clusterVersion *clusteroperator.ClusterVersion, masterSize int, computes ...clusteroperator.ClusterMachineSet) []*clusteroperator.MachineSet {
	machineSets := []*clusteroperator.MachineSet{}
	if masterSize > 0 {
		name := fmt.Sprintf("%s-master-random", cluster.Name)
		machineSet := newMachineSet(name, cluster, true)
		machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
		machineSet.Spec.Size = masterSize
		machineSet.Spec.Infra = true
		machineSet.Spec.ClusterVersionRef = corev1.ObjectReference{
			Name:      clusterVersion.Name,
			Namespace: clusterVersion.Namespace,
			UID:       clusterVersion.UID,
		}
		machineSet.Spec.ClusterHardware.AWS = cluster.Spec.Hardware.AWS
		machineSets = append(machineSets, machineSet)
	}
	for _, compute := range computes {
		name := fmt.Sprintf("%s-%s-random", cluster.Name, compute.Name)
		machineSet := newMachineSet(name, cluster, true)
		machineSet.Spec.MachineSetConfig = compute.MachineSetConfig
		machineSet.Spec.ClusterVersionRef = corev1.ObjectReference{
			Name:      clusterVersion.Name,
			Namespace: clusterVersion.Namespace,
			UID:       clusterVersion.UID,
		}
		machineSet.Spec.ClusterHardware.AWS = cluster.Spec.Hardware.AWS
		machineSets = append(machineSets, machineSet)
	}
	if store != nil {
		for _, machineSet := range machineSets {
			store.Add(machineSet)
		}
	}
	return machineSets
}

// newMachineSetsWithMasterInstanceType creates new machine sets, with the specific instance type for the master and
// stores them in the specified store.
func newMachineSetsWithMasterInstanceType(store cache.Store, cluster *clusteroperator.Cluster, clusterVersion *clusteroperator.ClusterVersion, masterInstanceType string, computes ...clusteroperator.ClusterMachineSet) []*clusteroperator.MachineSet {
	machineSets := []*clusteroperator.MachineSet{}
	if len(masterInstanceType) > 0 {
		name := fmt.Sprintf("%s-master-random", cluster.Name)
		machineSet := newMachineSet(name, cluster, true)
		machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
		machineSet.Spec.Size = 1
		machineSet.Spec.Hardware = &clusteroperator.MachineSetHardwareSpec{
			AWS: &clusteroperator.MachineSetAWSHardwareSpec{
				InstanceType: masterInstanceType,
			},
		}
		machineSet.Spec.Infra = true
		machineSet.Spec.ClusterVersionRef = corev1.ObjectReference{
			Name:      clusterVersion.Name,
			Namespace: clusterVersion.Namespace,
			UID:       clusterVersion.UID,
		}
		machineSet.Spec.ClusterHardware.AWS = cluster.Spec.Hardware.AWS
		machineSets = append(machineSets, machineSet)
	}
	for _, compute := range computes {
		name := fmt.Sprintf("%s-%s-random", cluster.Name, compute.Name)
		machineSet := newMachineSet(name, cluster, true)
		machineSet.Spec.MachineSetConfig = compute.MachineSetConfig
		machineSet.Spec.ClusterVersionRef = corev1.ObjectReference{
			Name:      clusterVersion.Name,
			Namespace: clusterVersion.Namespace,
			UID:       clusterVersion.UID,
		}
		machineSet.Spec.ClusterHardware.AWS = cluster.Spec.Hardware.AWS
		machineSets = append(machineSets, machineSet)
	}
	if store != nil {
		for _, machineSet := range machineSets {
			store.Add(machineSet)
		}
	}
	return machineSets
}

func newHardwareSpec(instanceType string) *clusteroperator.MachineSetHardwareSpec {
	return &clusteroperator.MachineSetHardwareSpec{
		AWS: &clusteroperator.MachineSetAWSHardwareSpec{
			InstanceType: instanceType,
		},
	}
}

// newClusterVer will create an actual ClusterVersion for the given reference.
// Used when we want to make sure a version ref specified on a Cluster exists in the store.
func newClusterVer(namespace, name string, uid types.UID) *clusteroperator.ClusterVersion {
	cv := &clusteroperator.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			UID:       uid,
			Name:      name,
			Namespace: namespace,
		},
		Spec: clusteroperator.ClusterVersionSpec{
			ImageFormat: "openshift/origin-${component}:${version}",
			VMImages: clusteroperator.VMImages{
				AWSImages: &clusteroperator.AWSVMImages{
					RegionAMIs: []clusteroperator.AWSRegionAMIs{
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

// processSync initiates a sync via processNextWorkItem() to test behavior that
// depends on both functions (such as re-queueing on sync error).
func processSync(c *Controller, key string) error {
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
	namePrefix   string
	hardwareSpec *clusteroperator.MachineSetHardwareSpec
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
	if machineSet.GenerateName != ea.namePrefix {
		return false
	}
	if ea.hardwareSpec != nil {
		if e, a := ea.hardwareSpec, machineSet.Spec.Hardware; !reflect.DeepEqual(e, a) {
			t.Errorf("unexpected hardware spec: expected %v, got %v", e, a)
		}
	}
	return true
}

// newExpectedMachineSetCreateAction creates a new expected client
// action for creating a machine set.
func newExpectedMachineSetCreateAction(cluster *clusteroperator.Cluster, name string) expectedMachineSetCreateAction {
	return newExpectedMachineSetCreateActionWithHardwareSpec(cluster, name, nil)
}

// expectedMachineSetUpdateAction is an expected client action to update a
// machine set.
type expectedMachineSetUpdateAction struct {
	namePrefix string
	size       int
}

func (ea expectedMachineSetUpdateAction) resource() schema.GroupVersionResource {
	return clusteroperator.SchemeGroupVersion.WithResource("machinesets")
}

func (ea expectedMachineSetUpdateAction) verb() string {
	return "update"
}

func (ea expectedMachineSetUpdateAction) validate(t *testing.T, action clientgotesting.Action) bool {
	updateAction, ok := action.(clientgotesting.UpdateAction)
	if !ok {
		t.Errorf("update action is not a UpdateAction: %t", updateAction)
		return false
	}
	updatedObject := updateAction.GetObject()
	machineSet, ok := updatedObject.(*clusteroperator.MachineSet)
	if !ok {
		t.Errorf("machine set update action object is not a MachineSet: %t", updatedObject)
		return false
	}
	if !strings.HasPrefix(machineSet.Name, ea.namePrefix) {
		return false
	}
	if e, a := ea.size, machineSet.Spec.Size; e != a {
		t.Errorf("unexpected size: expected %d, got %d", e, a)
		return false
	}
	return true
}

// newExpectedMachineSetUpdateAction creates a new expected client
// action for updating a machine set.
func newExpectedMachineSetUpdateAction(cluster *clusteroperator.Cluster, name string, size int) expectedMachineSetUpdateAction {
	return expectedMachineSetUpdateAction{namePrefix: cluster.Name + "-" + name, size: size}
}

// newExpectedMachineSetCreateActionWithHardwareSpec creates a new expected
// client action for creating a machine set that also validates the hardware
// spec in the machine set created.
func newExpectedMachineSetCreateActionWithHardwareSpec(
	cluster *clusteroperator.Cluster,
	name string,
	hardwareSpec *clusteroperator.MachineSetHardwareSpec,
) expectedMachineSetCreateAction {
	return expectedMachineSetCreateAction{
		namePrefix:   getNamePrefixForMachineSet(cluster, name),
		hardwareSpec: hardwareSpec,
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
	machineSets       *int
	clusterVersionRef *corev1.ObjectReference
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
	// TODO: This patch comparison needs a cleanup
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
	if ea.machineSets != nil {
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
		e, a := ea.machineSets, int(machineSetCount)
		assert.Equal(t, *e, a)
	}
	if ea.clusterVersionRef != nil {
		clusterVersionRefAsInterface := status["clusterVersionRef"]
		if clusterVersionRefAsInterface == nil {
			t.Errorf("expected status.clusterVersionRef field in patch")
			return true
		}
		clusterVersionRef, ok := clusterVersionRefAsInterface.(map[string]interface{})
		if !ok {
			t.Errorf("clusterVersionRef field is not an object")
			return true
		}

		nameAsInterface, ok := clusterVersionRef["name"]
		if !ok {
			t.Errorf("expected clusterVersionRef.name in patch")
			return true
		}
		name, ok := nameAsInterface.(string)
		if !ok {
			t.Errorf("clusterVersionRef.name is not a string")
			return true
		}
		assert.Equal(t, ea.clusterVersionRef.Name, name)

		uidAsInterface, ok := clusterVersionRef["uid"]
		if !ok {
			t.Errorf("expected clusterVersionRef.uid in patch")
			return true
		}
		uid, ok := uidAsInterface.(string)
		if !ok {
			t.Errorf("clusterVersionRef.uid is not a string")
			return true
		}
		assert.Equal(t, ea.clusterVersionRef.UID, types.UID(uid))
	}
	return true
}

// validateControllerExpectations validates that the specified cluster is
// expecting the specified number of creations and deletions of machine sets.
func validateControllerExpectations(t *testing.T, testName string, ctrlr *Controller, cluster *clusteroperator.Cluster, expectedAdds, expectedDeletes int) {
	expectations, ok, err := ctrlr.expectations.GetExpectations(getKey(cluster, t))
	switch {
	case err != nil:
		t.Errorf("%s: error getting expecations: %v", testName, err)
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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv, tc.computes...)
			cluster.Status.MachineSetCount = len(tc.computes) + 1
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, cv, true, tc.computes...)

			controller.syncCluster(getKey(cluster, t))

			validateClientActions(t, "TestSyncClusterSteadyState."+tc.name, clusterOperatorClient)

			validateControllerExpectations(t, "TestSyncClusterSteadyState."+tc.name, controller, cluster, 0, 0)
		})
	}
}

// TestSyncClusterWithVersion tests behaviour around the version set on a cluster spec.
func TestSyncClusterWithVersion(t *testing.T) {

	cases := []struct {
		name                       string
		currentClusterVersion      *clusteroperator.ClusterVersion
		newClusterVersion          *clusteroperator.ClusterVersion
		newClusterVersionExists    bool
		expectedMachineSetsCreated bool
		expectedMachineSetsDeleted bool
		expectedStatusUpdate       bool
		expectedStatusClusterRef   *corev1.ObjectReference
		expectedCondition          *clusteroperator.ClusterCondition
	}{
		{
			name: "NoCurrentVersionNewDoesNotExist",
			currentClusterVersion:      nil,
			newClusterVersion:          newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID),
			newClusterVersionExists:    false,
			expectedMachineSetsCreated: false,
			expectedMachineSetsDeleted: false,
			expectedStatusUpdate:       false,
			expectedStatusClusterRef:   nil,
		},
		{
			name: "NoCurrentVersionNewExists",
			currentClusterVersion:      nil,
			newClusterVersion:          newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID),
			newClusterVersionExists:    true,
			expectedMachineSetsCreated: true,
			expectedMachineSetsDeleted: false,
			expectedStatusUpdate:       true,
			expectedStatusClusterRef: &corev1.ObjectReference{
				Name:      testClusterVerName,
				Namespace: testClusterVerNS,
				UID:       testClusterVerUID,
			},
		},
		{
			name: "NoCurrentVersionMissingRegion",
			currentClusterVersion: nil,
			newClusterVersion: func() *clusteroperator.ClusterVersion {
				cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
				// Modify the default cluster version so it will not have a matching region:
				cv.Spec.VMImages.AWSImages.RegionAMIs[0].Region = "different-region"
				return cv
			}(),
			newClusterVersionExists:    true,
			expectedMachineSetsCreated: false,
			expectedMachineSetsDeleted: false,
			expectedStatusUpdate:       true,
			expectedStatusClusterRef: &corev1.ObjectReference{
				Name:      testClusterVerName,
				Namespace: testClusterVerNS,
				UID:       testClusterVerUID,
			},
			expectedCondition: &clusteroperator.ClusterCondition{
				Type:   clusteroperator.ClusterVersionIncompatible,
				Reason: versionMissingRegion,
				Status: corev1.ConditionTrue,
			},
		},
		{
			name: "NewVersionDoesNotExist",
			currentClusterVersion:      newClusterVer(testClusterVerNS, "currentversion", "currentUID"),
			newClusterVersion:          newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID),
			newClusterVersionExists:    false,
			expectedMachineSetsCreated: false,
			expectedMachineSetsDeleted: false,
			expectedStatusUpdate:       false,
			expectedStatusClusterRef: &corev1.ObjectReference{
				Name:      "currentversion",
				Namespace: testClusterVerNS,
				UID:       "currentUID",
			},
		},
		{
			name: "NewVersionExists",
			currentClusterVersion:      newClusterVer(testClusterVerNS, "currentversion", "currentUID"),
			newClusterVersion:          newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID),
			newClusterVersionExists:    true,
			expectedMachineSetsCreated: true,
			expectedMachineSetsDeleted: true,
			expectedStatusUpdate:       true,
			expectedStatusClusterRef: &corev1.ObjectReference{
				Name:      testClusterVerName,
				Namespace: testClusterVerNS,
				UID:       testClusterVerUID,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()
			computes := []string{"compute1", "compute2"}
			allMachineSets := []string{"master", "compute1", "compute2"}
			if tc.newClusterVersionExists {
				clusterVerStore.Add(tc.newClusterVersion)
			}
			cluster := newCluster(nil, computes...)
			cluster.Spec.ClusterVersionRef = clusteroperator.ClusterVersionReference{
				Name:      tc.newClusterVersion.Name,
				Namespace: tc.newClusterVersion.Namespace,
			}

			if tc.currentClusterVersion != nil {
				clusterVerStore.Add(tc.currentClusterVersion)
				cluster.Status.ClusterVersionRef = &corev1.ObjectReference{
					Name:      tc.currentClusterVersion.Name,
					Namespace: tc.currentClusterVersion.Namespace,
					UID:       tc.currentClusterVersion.UID,
				}
				// If the cluster has a current version we assume this means machine sets should
				// already exist, with that version:
				// Current version should be stamped onto cluster status and all machine sets:
				newMachineSets(machineSetStore, cluster, tc.currentClusterVersion, true,
					computes...)
				cluster.Status.MachineSetCount = 3
			}
			clusterStore.Add(cluster)

			err := controller.syncCluster(getKey(cluster, t))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tc.expectedStatusClusterRef == nil {
				assert.Zero(t, cluster.Status.ClusterVersionRef)
			} else {
				// TODO:
			}

			expectedActions := []expectedClientAction{}
			if tc.expectedMachineSetsCreated {
				for _, msName := range allMachineSets {
					expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, msName))
				}
			}
			if tc.expectedMachineSetsDeleted {
				for _, msName := range allMachineSets {
					expectedActions = append(expectedActions, newExpectedMachineSetDeleteAction(cluster, msName))
				}
			}
			if tc.expectedStatusUpdate {
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{
					clusterVersionRef: tc.expectedStatusClusterRef,
				})
			}

			expectedAdds, expectedDeletes := 0, 0
			if tc.expectedMachineSetsCreated {
				expectedAdds = len(allMachineSets)
			}
			if tc.expectedMachineSetsDeleted {
				expectedDeletes = len(allMachineSets)
			}

			validateClientActions(t, "TestSyncClusterWithVersion/"+tc.name, clusterOperatorClient, expectedActions...)
			validateControllerExpectations(t, "TestSyncClusterWithVersion/"+tc.name, controller, cluster, expectedAdds, expectedDeletes)

			if tc.expectedCondition != nil {
				foundCondition := false
				// NOTE: Only checking some fields on the condition for brevity in test data:
				for _, cond := range cluster.Status.Conditions {
					if cond.Type == tc.expectedCondition.Type && cond.Reason == tc.expectedCondition.Reason &&
						cond.Status == tc.expectedCondition.Status {
						foundCondition = true
						break
					}
				}
				assert.True(t, foundCondition, "missing expected condition")
			}
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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv, append(tc.existingComputes, tc.newComputes...)...)
			cluster.Status.MachineSetCount = len(tc.existingComputes) + 1
			if !tc.existingMaster {
				cluster.Status.MachineSetCount--
			}
			clusterStore.Add(cluster)

			newMachineSets(machineSetStore, cluster, cv, tc.existingMaster, tc.existingComputes...)

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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv, tc.computes...)
			cluster.Status.MachineSetCount = tc.oldComputes
			if !tc.masterAdded {
				cluster.Status.MachineSetCount++
			}
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, cv, true, tc.computes...)

			controller.syncCluster(getKey(cluster, t))

			expectedMSCount := 1 + len(tc.computes)
			validateClientActions(t, "TestSyncClusterMachineSetsAdded."+tc.name, clusterOperatorClient,
				expectedClusterStatusUpdateAction{machineSets: &expectedMSCount},
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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv, tc.clusterComputes...)
			cluster.Status.MachineSetCount = len(tc.clusterComputes) + 1
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, cv, !tc.masterDeleted, tc.realizedComputes...)

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
			expectedMSCount := len(tc.realizedComputes) + statusMasterMachineSets
			expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{
				machineSets: &expectedMSCount,
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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv, tc.clusterComputes...)
			cluster.Status.MachineSetCount = len(tc.clusterComputes) + 1
			clusterStore.Add(cluster)
			newMachineSets(machineSetStore, cluster, cv, true, tc.realizedComputes...)

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
		name                         string
		clusterMasterInstanceType    string
		realizedMasterInstanceType   string
		clusterComputeInstanceTypes  []string
		realizedComputeInstanceTypes []string
	}{
		{
			name: "master only",
			clusterMasterInstanceType:  "a",
			realizedMasterInstanceType: "b",
		},
		{
			name: "single compute",
			clusterMasterInstanceType:    "a",
			realizedMasterInstanceType:   "a",
			clusterComputeInstanceTypes:  []string{"b"},
			realizedComputeInstanceTypes: []string{"c"},
		},
		{
			name: "multiple computes",
			clusterMasterInstanceType:    "a",
			realizedMasterInstanceType:   "a",
			clusterComputeInstanceTypes:  []string{"b", "c"},
			realizedComputeInstanceTypes: []string{"d", "e"},
		},
		{
			name: "master and computes",
			clusterMasterInstanceType:    "a",
			realizedMasterInstanceType:   "b",
			clusterComputeInstanceTypes:  []string{"c", "d"},
			realizedComputeInstanceTypes: []string{"e", "f"},
		},
		{
			name: "subset of computes",
			clusterMasterInstanceType:    "a",
			realizedMasterInstanceType:   "b",
			clusterComputeInstanceTypes:  []string{"c", "d", "e"},
			realizedComputeInstanceTypes: []string{"g", "h", "e"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if a, b := len(tc.clusterComputeInstanceTypes), len(tc.realizedComputeInstanceTypes); a != b {
				t.Skipf("clusterComputeInstanceTypes length must be equal to realizedComputeInstanceTypes length: %v, %v", a, b)
			}

			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			clusterComputes := make([]clusteroperator.ClusterMachineSet, len(tc.clusterComputeInstanceTypes))
			realizedComputes := make([]clusteroperator.ClusterMachineSet, len(tc.realizedComputeInstanceTypes))
			for i := range tc.clusterComputeInstanceTypes {
				name := fmt.Sprintf("compute%v", i)
				clusterComputes[i] = clusteroperator.ClusterMachineSet{
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: clusteroperator.NodeTypeCompute,
						Size:     1,
						Hardware: &clusteroperator.MachineSetHardwareSpec{
							AWS: &clusteroperator.MachineSetAWSHardwareSpec{
								InstanceType: tc.clusterComputeInstanceTypes[i],
							},
						},
					},
					Name: name,
				}
				realizedComputes[i] = clusteroperator.ClusterMachineSet{
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: clusteroperator.NodeTypeCompute,
						Size:     1,
						Hardware: &clusteroperator.MachineSetHardwareSpec{
							AWS: &clusteroperator.MachineSetAWSHardwareSpec{
								InstanceType: tc.realizedComputeInstanceTypes[i],
							},
						},
					},
					Name: name,
				}
			}
			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			cluster := newClusterWithMasterInstanceType(cv, tc.clusterMasterInstanceType, clusterComputes...)
			cluster.Status.MachineSetCount = len(clusterComputes) + 1
			clusterVerStore.Add(cv)
			clusterStore.Add(cluster)
			newMachineSetsWithMasterInstanceType(machineSetStore, cluster, cv, tc.realizedMasterInstanceType, realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.clusterMasterInstanceType != tc.realizedMasterInstanceType {
				expectedActions = append(expectedActions,
					newExpectedMachineSetDeleteAction(cluster, "master"),
					newExpectedMachineSetCreateAction(cluster, "master"),
				)
			}
			for i, clusterCompute := range clusterComputes {
				if clusterCompute.Hardware.AWS.InstanceType == tc.realizedComputeInstanceTypes[i] {
					continue
				}
				expectedActions = append(expectedActions,
					newExpectedMachineSetDeleteAction(cluster, clusterCompute.Name),
					newExpectedMachineSetCreateAction(cluster, clusterCompute.Name),
				)
			}

			validateClientActions(t, "TestSyncClusterMachineSetSpecMutated."+tc.name, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, "TestSyncClusterMachineSetSpecMutated."+tc.name, controller, cluster, len(expectedActions)/2, len(expectedActions)/2)
		})
	}
}

// TestSyncClusterMachineSetSpecScaled tests syncing a cluster when machine set
// sizes in the cluster spec have been mutated.
func TestSyncClusterMachineSetSpecScaled(t *testing.T) {
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

			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

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
			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			cluster := newClusterWithSizes(cv, tc.clusterMasterSize, clusterComputes...)
			cluster.Status.MachineSetCount = len(clusterComputes) + 1
			clusterVerStore.Add(cv)
			clusterStore.Add(cluster)
			newMachineSetsWithSizes(machineSetStore, cluster, cv, tc.realizedMasterSize, realizedComputes...)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.clusterMasterSize != tc.realizedMasterSize {
				expectedActions = append(expectedActions,
					newExpectedMachineSetUpdateAction(cluster, "master", tc.clusterMasterSize),
				)
			}
			for i, clusterCompute := range clusterComputes {
				if clusterCompute.Size == tc.realizedComputeSizes[i] {
					continue
				}
				expectedActions = append(expectedActions,
					newExpectedMachineSetUpdateAction(cluster, clusterCompute.Name, clusterCompute.Size),
				)
			}

			validateClientActions(t, "TestSyncClusterMachineSetSpecScaled."+tc.name, clusterOperatorClient, expectedActions...)
		})
	}
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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv)
			clusterStore.Add(cluster)

			machineSetName := fmt.Sprintf("%s-master-random", cluster.Name)
			machineSet := newMachineSet(machineSetName, cluster, false)
			machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
			machineSet.Spec.Size = 1
			machineSet.Spec.Infra = true
			machineSet.Spec.ClusterVersionRef = *cluster.Status.ClusterVersionRef
			machineSet.Spec.ClusterHardware = cluster.Spec.Hardware
			if tc.ownerRef != nil {
				machineSet.OwnerReferences = []metav1.OwnerReference{*tc.ownerRef}
			}
			machineSetStore.Add(machineSet)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.expectNewMaster {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, "master"))
			} else {
				expectedMSCount := 1
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{machineSets: &expectedMSCount})
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
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newCluster(cv)
			clusterStore.Add(cluster)

			machineSetName := fmt.Sprintf("%s-master-random", cluster.Name)
			machineSet := newMachineSet(machineSetName, cluster, true)
			machineSet.Spec.NodeType = clusteroperator.NodeTypeMaster
			machineSet.Spec.Size = 1
			machineSet.Spec.Infra = true
			machineSet.Spec.ClusterVersionRef = *cluster.Status.ClusterVersionRef
			machineSet.Spec.ClusterHardware = cluster.Spec.Hardware
			machineSet.DeletionTimestamp = tc.deletionTimestamp
			machineSetStore.Add(machineSet)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			if tc.expectNewMaster {
				expectedActions = append(expectedActions, newExpectedMachineSetCreateAction(cluster, "master"))
			} else {
				expectedMSCount := 1
				expectedActions = append(expectedActions, expectedClusterStatusUpdateAction{machineSets: &expectedMSCount})
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
	controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

	cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
	clusterVerStore.Add(cv)
	cluster := newClusterWithMasterInstanceType(cv, "a",
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
				Hardware: &clusteroperator.MachineSetHardwareSpec{
					AWS: &clusteroperator.MachineSetAWSHardwareSpec{
						InstanceType: "a",
					},
				},
			},
			Name: "realized and un-mutated",
		},
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
				Hardware: &clusteroperator.MachineSetHardwareSpec{
					AWS: &clusteroperator.MachineSetAWSHardwareSpec{
						InstanceType: "a",
					},
				},
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

	newMachineSetsWithMasterInstanceType(machineSetStore, cluster, cv, "b",
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     1,
				NodeType: clusteroperator.NodeTypeCompute,
				Hardware: &clusteroperator.MachineSetHardwareSpec{
					AWS: &clusteroperator.MachineSetAWSHardwareSpec{
						InstanceType: "a",
					},
				},
			},
			Name: "realized and un-mutated",
		},
		clusteroperator.ClusterMachineSet{
			MachineSetConfig: clusteroperator.MachineSetConfig{
				Size:     2,
				NodeType: clusteroperator.NodeTypeCompute,
				Hardware: &clusteroperator.MachineSetHardwareSpec{
					AWS: &clusteroperator.MachineSetAWSHardwareSpec{
						InstanceType: "b",
					},
				},
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

	expectedMSCount := 3
	validateClientActions(t, "TestSyncClusterComplex", clusterOperatorClient,
		newExpectedMachineSetDeleteAction(cluster, "master"),
		newExpectedMachineSetCreateAction(cluster, "master"),
		newExpectedMachineSetDeleteAction(cluster, "realized but mutated"),
		newExpectedMachineSetCreateAction(cluster, "realized but mutated"),
		newExpectedMachineSetCreateAction(cluster, "unrealized"),
		newExpectedMachineSetDeleteAction(cluster, "removed from cluster"),
		expectedClusterStatusUpdateAction{machineSets: &expectedMSCount}, // status only counts the 2 realized compute nodes + 1 master
	)

	validateControllerExpectations(t, "TestSyncClusterComplex", controller, cluster, 3, 3)
}

// TestSyncClusterMachineSetsHardwareChange tests syncing a cluster when the
// hardware spec has changed.
func TestSyncClusterMachineSetsHardwareChange(t *testing.T) {
	cases := []struct {
		name             string
		old              *clusteroperator.MachineSetHardwareSpec
		newDefault       *clusteroperator.MachineSetHardwareSpec
		newForMachineSet *clusteroperator.MachineSetHardwareSpec
		expected         *clusteroperator.MachineSetHardwareSpec
	}{
		{
			name:             "hardware from defaults",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec("instance-type"),
			newForMachineSet: newHardwareSpec(""),
		},
		{
			name:             "hardware from machine set",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec(""),
			newForMachineSet: newHardwareSpec("instance-type"),
		},
		{
			name:             "hardware mixed from default and machine set",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec("instance-type"),
			newForMachineSet: newHardwareSpec(""),
		},
		{
			name:             "hardware from machine set overriding default",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec("overriden-instance-type"),
			newForMachineSet: newHardwareSpec("instance-type"),
		},
		{
			name:             "instance type changed for machine set",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec(""),
			newForMachineSet: newHardwareSpec("new-instance-type"),
			expected:         newHardwareSpec("new-instance-type"),
		},
		{
			name:             "instance type changed for default",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec("new-instance-type"),
			newForMachineSet: newHardwareSpec(""),
			expected:         newHardwareSpec("new-instance-type"),
		},
		{
			name:             "changed by overriding for machine set",
			old:              newHardwareSpec("instance-type"),
			newDefault:       newHardwareSpec("instance-type"),
			newForMachineSet: newHardwareSpec("new-instance-type"),
			expected:         newHardwareSpec("new-instance-type"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, machineSetStore, clusterVerStore, _, clusterOperatorClient := newTestController()

			cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
			clusterVerStore.Add(cv)
			cluster := newClusterWithSizes(cv, 1)
			cluster.Status.MachineSetCount = 1
			cluster.Spec.DefaultHardwareSpec = tc.newDefault
			cluster.Spec.MachineSets[0].Hardware = tc.newForMachineSet
			clusterStore.Add(cluster)
			machineSets := newMachineSetsWithSizes(nil, cluster, cv, 1)
			masterMachineSet := machineSets[0]
			masterMachineSet.Spec.Hardware = tc.old
			machineSetStore.Add(masterMachineSet)

			controller.syncCluster(getKey(cluster, t))

			expectedActions := []expectedClientAction{}
			expectedAdditions := 0
			expectedDeletions := 0
			if tc.expected != nil {
				expectedActions = append(expectedActions,
					newExpectedMachineSetDeleteAction(cluster, "master"),
					newExpectedMachineSetCreateActionWithHardwareSpec(cluster, "master", tc.expected),
				)
				expectedAdditions = 1
				expectedDeletions = 1
			}

			validateClientActions(t, "TestSyncClusterMachineSetsHardwareChange."+tc.name, clusterOperatorClient, expectedActions...)

			validateControllerExpectations(t, "TestSyncClusterMachineSetsMutated."+tc.name, controller, cluster, expectedAdditions, expectedDeletions)
		})
	}
}
