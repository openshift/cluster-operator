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

package clusterdeployment

import (
	"bytes"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	clustop "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	clustopinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	"github.com/openshift/cluster-operator/pkg/controller"

	capi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"
)

const (
	testNamespace             = "test-namespace"
	testClusterDeploymentName = "test-cluster-deployment"
	testClusterDeploymentUUID = types.UID("test-cluster-deployment-uuid")
	testClusterName           = "test-cluster-deployment"
	testClusterUUID           = types.UID("test-cluster-uuid")
	testClusterVerName        = "v3-9"
	testClusterVerNS          = "cluster-operator"
	testClusterVerUID         = types.UID("test-cluster-version")
	testRegion                = "us-east-1"
)

func TestSync(t *testing.T) {
	tests := []struct {
		name                   string
		clusterDeployment      *clustop.ClusterDeployment
		clusterVersion         *clustop.ClusterVersion
		existingCluster        *capi.Cluster
		existingMachineSet     *capi.MachineSet
		expectedCAPIActions    []expectedClientAction
		expectedClustopActions []expectedClientAction
		expectErr              bool
	}{
		{
			name:              "create new cluster",
			clusterDeployment: testClusterDeployment(),
			clusterVersion:    testClusterVersion(),
			expectedCAPIActions: []expectedClientAction{
				clusterCreatedAction{
					clusterDeployment: testClusterDeployment(),
				},
			},
		},
		{
			name:              "create master machineset",
			clusterDeployment: testClusterDeployment(),
			clusterVersion:    testClusterVersion(),
			existingCluster:   provisionedCluster(),
			expectedCAPIActions: []expectedClientAction{
				machineSetCreatedAction{
					clusterVersion:    testClusterVersion(),
					cluster:           provisionedCluster(),
					clusterDeployment: testClusterDeployment(),
				},
			},
		},
		{
			name:                "steady state",
			clusterDeployment:   testClusterDeployment(),
			clusterVersion:      testClusterVersion(),
			existingCluster:     provisionedCluster(),
			existingMachineSet:  testMasterMachineSet(),
			expectedCAPIActions: []expectedClientAction{},
		},
		{
			name:              "missing cluster version",
			clusterDeployment: testClusterDeployment(),
			clusterVersion:    nil,
			expectedClustopActions: []expectedClientAction{
				clusterDeploymentStatusUpdateAction{
					clusterDeployment: testClusterDeployment(),
					condition:         clustop.ClusterVersionMissing,
				},
			},
		},
		{
			name:              "incompatible cluster version",
			clusterDeployment: alternateRegionClusterDeployment(),
			clusterVersion:    testClusterVersion(),
			expectedClustopActions: []expectedClientAction{
				clusterDeploymentStatusUpdateAction{
					clusterDeployment: alternateRegionClusterDeployment(),
					condition:         clustop.ClusterVersionIncompatible,
				},
			},
		},
		{
			name:               "update cluster and master machineset",
			clusterDeployment:  testClusterDeploymentWith3Masters(),
			clusterVersion:     testClusterVersion(),
			existingCluster:    testCluster(),
			existingMachineSet: testMasterMachineSet(),
			expectedCAPIActions: []expectedClientAction{
				clusterUpdateAction{
					clusterDeployment: testClusterDeploymentWith3Masters(),
				},
				machineSetUpdateAction{
					clusterDeployment: testClusterDeploymentWith3Masters(),
					machineSetConfig:  &testClusterDeploymentWith3Masters().Spec.MachineSets[0].MachineSetConfig,
					clusterVersion:    testClusterVersion(),
				},
			},
		},
	}

	for _, tc := range tests {
		ctx := setupTest()
		clusterDeployment := tc.clusterDeployment
		ctx.clusterDeploymentStore.Add(clusterDeployment)
		if tc.clusterVersion != nil {
			ctx.clusterVersionStore.Add(testClusterVersion())
		}
		if tc.existingCluster != nil {
			ctx.clusterStore.Add(tc.existingCluster)
		}
		if tc.existingMachineSet != nil {
			ctx.machineSetStore.Add(tc.existingMachineSet)
		}
		err := ctx.controller.syncClusterDeployment(getKey(clusterDeployment, t))
		if err != nil {
			if !tc.expectErr {
				t.Errorf("%s: unexpected: %v", tc.name, err)
			}
		}
		if err == nil && tc.expectErr {
			t.Errorf("%s: expected error", tc.name)
		}
		if tc.expectedCAPIActions != nil {
			validateClientActions(t, tc.name, &ctx.capiClient.Fake, tc.expectedCAPIActions...)
		}
		if tc.expectedClustopActions != nil {
			validateClientActions(t, tc.name, &ctx.clustopClient.Fake, tc.expectedClustopActions...)
		}
	}
}

type testContext struct {
	controller             *Controller
	clusterDeploymentStore cache.Store
	clusterVersionStore    cache.Store
	clusterStore           cache.Store
	machineSetStore        cache.Store
	clustopClient          *clustopclient.Clientset
	capiClient             *capiclient.Clientset
	kubeClient             *clientgofake.Clientset
}

func setupTest() *testContext {
	kubeClient := &clientgofake.Clientset{}
	clustopClient := &clustopclient.Clientset{}
	capiClient := &capiclient.Clientset{}

	clustopInformers := clustopinformers.NewSharedInformerFactory(clustopClient, 0)
	capiInformers := capiinformers.NewSharedInformerFactory(capiClient, 0)

	ctx := &testContext{
		controller: NewController(
			clustopInformers.Clusteroperator().V1alpha1().ClusterDeployments(),
			capiInformers.Cluster().V1alpha1().Clusters(),
			capiInformers.Cluster().V1alpha1().MachineSets(),
			clustopInformers.Clusteroperator().V1alpha1().ClusterVersions(),
			kubeClient,
			clustopClient,
			capiClient,
		),
		clusterDeploymentStore: clustopInformers.Clusteroperator().V1alpha1().ClusterDeployments().Informer().GetStore(),
		clusterVersionStore:    clustopInformers.Clusteroperator().V1alpha1().ClusterVersions().Informer().GetStore(),
		clusterStore:           capiInformers.Cluster().V1alpha1().Clusters().Informer().GetStore(),
		machineSetStore:        capiInformers.Cluster().V1alpha1().MachineSets().Informer().GetStore(),
		clustopClient:          clustopClient,
		kubeClient:             kubeClient,
		capiClient:             capiClient,
	}
	return ctx
}

// validateClientActions validates that the client experienced the specified
// expected actions.
func validateClientActions(t *testing.T, testName string, client *clientgotesting.Fake, expectedActions ...expectedClientAction) {
	actualActions := client.Actions()
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
			if expectedAction.validate(t, testName, actualAction) {
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
	validate(t *testing.T, testName string, action clientgotesting.Action) bool
}

// testClusterDeployment creates a new test ClusterDeployment
func testClusterDeployment() *clustop.ClusterDeployment {
	clusterDeployment := &clustop.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterDeploymentUUID,
			Name:      testClusterDeploymentName,
			Namespace: testNamespace,
		},
		Spec: clustop.ClusterDeploymentSpec{
			MachineSets: []clustop.ClusterMachineSet{
				{
					ShortName: "",
					MachineSetConfig: clustop.MachineSetConfig{
						Infra:    true,
						Size:     1,
						NodeType: clustop.NodeTypeMaster,
					},
				},
				{
					ShortName: "compute",
					MachineSetConfig: clustop.MachineSetConfig{
						Infra:    false,
						Size:     1,
						NodeType: clustop.NodeTypeCompute,
					},
				},
			},
			Hardware: clustop.ClusterHardwareSpec{
				AWS: &clustop.AWSClusterSpec{
					SSHUser: "clusteroperator",
					Region:  testRegion,
				},
			},
			ClusterVersionRef: clustop.ClusterVersionReference{
				Name:      testClusterVerName,
				Namespace: testClusterVerNS,
			},
		},
	}
	return clusterDeployment
}

func alternateRegionClusterDeployment() *clustop.ClusterDeployment {
	clusterDeployment := testClusterDeployment()
	clusterDeployment.Spec.Hardware.AWS.Region = "alternate-region"
	return clusterDeployment
}

func testClusterDeploymentWith3Masters() *clustop.ClusterDeployment {
	clusterDeployment := testClusterDeployment()
	clusterDeployment.Spec.MachineSets[0].Size = 3
	return clusterDeployment
}

func testCluster() *capi.Cluster {
	clusterDeployment := testClusterDeployment()
	cluster, _ := buildCluster(clusterDeployment)
	return cluster
}

func provisionedCluster() *capi.Cluster {
	cluster := testCluster()
	clusterStatus, _ := controller.ClusterStatusFromClusterAPI(cluster)
	clusterStatus.Provisioned = true
	cluster.Status.ProviderStatus, _ = controller.ClusterAPIProviderStatusFromClusterStatus(clusterStatus)
	return cluster
}

// getKey gets the key for the cluster to use when checking expectations
// set on a cluster.
func getKey(obj metav1.Object, t *testing.T) string {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		t.Errorf("Unexpected error getting key for resource %v: %v", obj.GetName(), err)
		return ""
	}
	return key
}

// testClusterVersion will create a ClusterVersion resource.
// Used when we want to make sure a version ref specified on a Cluster exists in the store.
func testClusterVersion() *clustop.ClusterVersion {
	cv := &clustop.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterVerUID,
			Name:      testClusterVerName,
			Namespace: testClusterVerNS,
		},
		Spec: clustop.ClusterVersionSpec{
			ImageFormat: "openshift/origin-${component}:${version}",
			VMImages: clustop.VMImages{
				AWSImages: &clustop.AWSVMImages{
					RegionAMIs: []clustop.AWSRegionAMIs{
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

// testMasterMachineSet will create a master MachineSet resource
func testMasterMachineSet() *capi.MachineSet {
	clusterDeployment := testClusterDeployment()
	cluster := testCluster()
	clusterVersion := testClusterVersion()
	ms, _ := buildMasterMachineSet(clusterDeployment, cluster, clusterVersion)
	return ms
}

type clusterCreatedAction struct {
	clusterDeployment *clustop.ClusterDeployment
}

func (a clusterCreatedAction) resource() schema.GroupVersionResource {
	return capi.SchemeGroupVersion.WithResource("clusters")
}

func (a clusterCreatedAction) verb() string {
	return "create"
}

func (a clusterCreatedAction) validate(t *testing.T, testName string, action clientgotesting.Action) bool {
	createAction, ok := action.(clientgotesting.CreateAction)
	if !ok {
		t.Errorf("%s: action is not a create action: %t", testName, action)
		return false
	}
	createdObject := createAction.GetObject()
	cluster, ok := createdObject.(*capi.Cluster)
	if !ok {
		t.Errorf("%s: created object is not a cluster: %t", testName, createdObject)
		return false
	}
	clusterDeploymentLabel, ok := cluster.Labels[clusterDeploymentLabel]
	if !ok {
		t.Errorf("%s: no cluster deployment label present in cluster %s/%s", testName, cluster.Namespace, cluster.Name)
		return false
	}
	if clusterDeploymentLabel != a.clusterDeployment.Name {
		t.Errorf("%s: cluster deployment label does not match cluster deployment name", testName)
		return false
	}
	providerConfig, err := controller.ClusterProviderConfigSpecFromClusterDeploymentSpec(&a.clusterDeployment.Spec)
	if err != nil {
		t.Errorf("%s: cannot obtain provider config from cluster deployment: %v", testName, err)
		return false
	}
	if !bytes.Equal(cluster.Spec.ProviderConfig.Value.Raw, providerConfig.Raw) {
		t.Errorf("%s: provider config of created cluster does not match cluster deployment spec", testName)
		return false
	}
	return true
}

type clusterUpdateAction struct {
	clusterDeployment *clustop.ClusterDeployment
}

func (a clusterUpdateAction) resource() schema.GroupVersionResource {
	return capi.SchemeGroupVersion.WithResource("clusters")
}

func (a clusterUpdateAction) verb() string {
	return "update"
}

func (a clusterUpdateAction) validate(t *testing.T, testName string, action clientgotesting.Action) bool {
	updateAction, ok := action.(clientgotesting.UpdateAction)
	if !ok {
		t.Errorf("%s: action is not a update action: %t", testName, action)
		return false
	}
	updatedObject := updateAction.GetObject()
	cluster, ok := updatedObject.(*capi.Cluster)
	if !ok {
		t.Errorf("%s: updated object is not a cluster: %t", testName, updatedObject)
		return false
	}
	providerConfig, err := controller.ClusterProviderConfigSpecFromClusterDeploymentSpec(&a.clusterDeployment.Spec)
	if err != nil {
		t.Errorf("%s: cannot obtain provider config from cluster deployment: %v", testName, err)
		return false
	}
	if !bytes.Equal(cluster.Spec.ProviderConfig.Value.Raw, providerConfig.Raw) {
		t.Errorf("%s: provider config of updated cluster does not match expected deployment spec", testName)
		return false
	}
	return true
}

type machineSetCreatedAction struct {
	clusterDeployment *clustop.ClusterDeployment
	clusterVersion    *clustop.ClusterVersion
	cluster           *capi.Cluster
}

func (a machineSetCreatedAction) resource() schema.GroupVersionResource {
	return capi.SchemeGroupVersion.WithResource("machinesets")
}

func (a machineSetCreatedAction) verb() string {
	return "create"
}

func (a machineSetCreatedAction) validate(t *testing.T, testName string, action clientgotesting.Action) bool {
	createAction, ok := action.(clientgotesting.CreateAction)
	if !ok {
		t.Errorf("%s: action is not a create action: %t", testName, action)
		return false
	}
	createdObject := createAction.GetObject()
	machineSet, ok := createdObject.(*capi.MachineSet)
	if !ok {
		t.Errorf("%s: created object is not a machineset: %t", testName, createdObject)
		return false
	}
	clusterDeploymentLabel, ok := machineSet.Labels[clusterDeploymentLabel]
	if !ok {
		t.Errorf("%s: no cluster deployment label present in machineset %s/%s", testName, machineSet.Namespace, machineSet.Name)
		return false
	}
	if clusterDeploymentLabel != a.clusterDeployment.Name {
		t.Errorf("%s: cluster deployment label does not match cluster deployment name", testName)
		return false
	}
	machineSetConfig, _ := masterMachineSetConfig(a.clusterDeployment)
	providerConfig, err := controller.MachineProviderConfigFromMachineSetConfig(machineSetConfig, &a.clusterDeployment.Spec, a.clusterVersion)
	if err != nil {
		t.Errorf("%s: cannot obtain provider config from cluster deployment: %v", testName, err)
		return false
	}
	if !bytes.Equal(machineSet.Spec.Template.Spec.ProviderConfig.Value.Raw, providerConfig.Raw) {
		t.Errorf("%s: provider config of created machine set does not match the one calculated from the cluster deployment spec", testName)
		return false
	}
	return true
}

type machineSetUpdateAction struct {
	clusterDeployment *clustop.ClusterDeployment
	machineSetConfig  *clustop.MachineSetConfig
	clusterVersion    *clustop.ClusterVersion
}

func (a machineSetUpdateAction) resource() schema.GroupVersionResource {
	return capi.SchemeGroupVersion.WithResource("machinesets")
}

func (a machineSetUpdateAction) verb() string {
	return "update"
}

func (a machineSetUpdateAction) validate(t *testing.T, testName string, action clientgotesting.Action) bool {
	updateAction, ok := action.(clientgotesting.UpdateAction)
	if !ok {
		t.Errorf("%s: action is not an update action: %t", testName, action)
	}

	updatedObject := updateAction.GetObject()
	machineSet, ok := updatedObject.(*capi.MachineSet)
	if !ok {
		t.Errorf("%s: updated object is not a machineset: %t", testName, updatedObject)
		return false
	}

	if *machineSet.Spec.Replicas != int32(a.machineSetConfig.Size) {
		t.Errorf("%s: updated machineset does not have the expected replica size: %d", testName, machineSet.Spec.Replicas)
		return false
	}

	providerConfig, err := controller.MachineProviderConfigFromMachineSetConfig(a.machineSetConfig, &a.clusterDeployment.Spec, a.clusterVersion)
	if err != nil {
		t.Errorf("%s: unable to get provider config from machineset config: %v", testName, err)
		return false
	}
	if !bytes.Equal(machineSet.Spec.Template.Spec.ProviderConfig.Value.Raw, providerConfig.Raw) {
		t.Errorf("%s: updated machineset's provider config is not a the expected value", testName)
		return false
	}

	return true
}

type clusterDeploymentStatusUpdateAction struct {
	clusterDeployment *clustop.ClusterDeployment
	condition         clustop.ClusterConditionType
}

func (a clusterDeploymentStatusUpdateAction) resource() schema.GroupVersionResource {
	return clustop.SchemeGroupVersion.WithResource("clusterdeployments")
}

func (a clusterDeploymentStatusUpdateAction) verb() string {
	return "patch"
}

func (a clusterDeploymentStatusUpdateAction) validate(t *testing.T, testName string, action clientgotesting.Action) bool {
	patchAction, ok := action.(clientgotesting.PatchAction)
	if !ok {
		t.Errorf("%s: action is not a patch action: %t", testName, action)
		return false
	}
	if patchAction.GetSubresource() != "status" {
		t.Errorf("%s: action does not match status subresource", testName)
	}
	patch := patchAction.GetPatch()
	original, err := json.Marshal(a.clusterDeployment)
	if err != nil {
		t.Errorf("%s: cannot marshal cluster deployment: %v", testName, err)
		return false
	}
	result, err := strategicpatch.StrategicMergePatch(original, patch, &clustop.ClusterDeployment{})
	if err != nil {
		t.Errorf("%s: cannot apply strategic merge patch: %v", testName, err)
		return false
	}
	resultClusterDeployment := &clustop.ClusterDeployment{}
	err = json.Unmarshal(result, resultClusterDeployment)
	if err != nil {
		t.Errorf("%s: cannot unmarshal cluster deployment: %v", testName, err)
		return false
	}

	clusterDeploymentCondition := controller.FindClusterCondition(&resultClusterDeployment.Status, a.condition)
	if clusterDeploymentCondition == nil {
		t.Errorf("%s: did not find expected cluster condition %s", testName, a.condition)
		return false
	}
	if clusterDeploymentCondition.Status != corev1.ConditionTrue {
		t.Errorf("%s: condition %s is not set to true", testName, a.condition)
	}
	return true
}
