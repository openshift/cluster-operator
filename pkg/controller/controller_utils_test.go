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

package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

const (
	testAMI             = "computeAMI_ID"
	testRegion          = "us-east-1"
	defaultInstanceType = "t2.xlarge"
)

// TestUpdateConditionAlways tests the UpdateConditionAlways function.
func TestUpdateConditionAlways(t *testing.T) {
	cases := []struct {
		name       string
		oldReason  string
		oldMessage string
		newReason  string
		newMessage string
	}{
		{
			name:       "all same",
			oldReason:  "same",
			oldMessage: "same",
			newReason:  "same",
			newMessage: "same",
		},
		{
			name:       "all different",
			oldReason:  "first",
			oldMessage: "second",
			newReason:  "third",
			newMessage: "fourth",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update := UpdateConditionAlways(tc.oldReason, tc.oldMessage, tc.newReason, tc.newMessage)
			assert.True(t, update)
		})
	}
}

// TestUpdateConditionNever tests the UpdateConditionNever function.
func TestUpdateConditionNever(t *testing.T) {
	cases := []struct {
		name       string
		oldReason  string
		oldMessage string
		newReason  string
		newMessage string
	}{
		{
			name:       "all same",
			oldReason:  "same",
			oldMessage: "same",
			newReason:  "same",
			newMessage: "same",
		},
		{
			name:       "all different",
			oldReason:  "first",
			oldMessage: "second",
			newReason:  "third",
			newMessage: "fourth",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update := UpdateConditionNever(tc.oldReason, tc.oldMessage, tc.newReason, tc.newMessage)
			assert.False(t, update)
		})
	}
}

// TestUpdateConditionIfReasonOrMessageChange tests the
// UpdateConditionIfReasonOrMessageChange function.
func TestUpdateConditionIfReasonOrMessageChange(t *testing.T) {
	cases := []struct {
		name       string
		oldReason  string
		oldMessage string
		newReason  string
		newMessage string
		expected   bool
	}{
		{
			name:       "all same",
			oldReason:  "same",
			oldMessage: "same",
			newReason:  "same",
			newMessage: "same",
			expected:   false,
		},
		{
			name:       "all different",
			oldReason:  "first",
			oldMessage: "second",
			newReason:  "third",
			newMessage: "fourth",
			expected:   true,
		},
		{
			name:       "different reason",
			oldReason:  "old reason",
			oldMessage: "message",
			newReason:  "new reason",
			newMessage: "message",
			expected:   true,
		},
		{
			name:       "different message",
			oldReason:  "reason",
			oldMessage: "old message",
			newReason:  "reason",
			newMessage: "new message",
			expected:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update := UpdateConditionIfReasonOrMessageChange(tc.oldReason, tc.oldMessage, tc.newReason, tc.newMessage)
			assert.Equal(t, tc.expected, update)
		})
	}
}

func newClusterCondition(
	conditionType cov1.ClusterConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	lastTransitionTime metav1.Time,
	lastProbeTime metav1.Time,
) cov1.ClusterCondition {
	return cov1.ClusterCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: lastTransitionTime,
		LastProbeTime:      lastProbeTime,
	}
}

// TestSetClusterCondition tests the SetClusterCondtion function.
func TestSetClusterCondition(t *testing.T) {
	cases := []struct {
		name               string
		existingConditions []cov1.ClusterCondition
		conditionType      cov1.ClusterConditionType
		status             corev1.ConditionStatus
		reason             string
		message            string
		updateCondition    bool
		expectedConditions []cov1.ClusterCondition
		expectedOldReason  string
		expectedOldMessage string
	}{
		{
			name:          "new condition",
			conditionType: cov1.ClusterReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "reason", "message", metav1.Now(), metav1.Now()),
			},
		},
		{
			name: "new condition with existing conditions",
			existingConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterInfraProvisioning, corev1.ConditionTrue, "other reason", "other message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: cov1.ClusterReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterInfraProvisioning, corev1.ConditionTrue, "other reason", "other message", metav1.Time{}, metav1.Time{}),
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "reason", "message", metav1.Now(), metav1.Now()),
			},
		},
		{
			name:          "false condition not created",
			conditionType: cov1.ClusterReady,
			status:        corev1.ConditionFalse,
			reason:        "reason",
			message:       "message",
		},
		{
			name: "condition not updated",
			existingConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: cov1.ClusterReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
		{
			name: "condition status changed",
			existingConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: cov1.ClusterReady,
			status:        corev1.ConditionFalse,
			reason:        "reason",
			message:       "message",
			expectedConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionFalse, "reason", "message", metav1.Now(), metav1.Now()),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
		{
			name: "condition changed due to update check",
			existingConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType:   cov1.ClusterReady,
			status:          corev1.ConditionTrue,
			reason:          "reason",
			message:         "message",
			updateCondition: true,
			expectedConditions: []cov1.ClusterCondition{
				newClusterCondition(cov1.ClusterReady, corev1.ConditionTrue, "reason", "message", metav1.Time{}, metav1.Now()),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updateCheck := func(oldReason, oldMessage, newReason, newMessage string) bool {
				assert.Equal(t, tc.expectedOldReason, oldReason, "unexpected old reason passed to update condition check")
				assert.Equal(t, tc.expectedOldMessage, oldMessage, "unexpected old message passed to update condition check")
				assert.Equal(t, tc.reason, newReason, "unexpected new reason passed to update condition check")
				assert.Equal(t, tc.message, newMessage, "unexpected new message passed to update condition check")
				return tc.updateCondition
			}
			startTime := time.Now()
			actualConditions := SetClusterCondition(
				tc.existingConditions,
				tc.conditionType,
				tc.status,
				tc.reason,
				tc.message,
				updateCheck,
			)
			endTime := time.Now()
			if assert.Equal(t, len(tc.expectedConditions), len(actualConditions), "unexpected number of conditions") {
				for i := range actualConditions {
					expected := tc.expectedConditions[i]
					actual := actualConditions[i]
					assert.Equal(t, expected.Type, actual.Type, "unexpected type in condition %d", i)
					assert.Equal(t, expected.Status, actual.Status, "unexpected status in condition %d", i)
					assert.Equal(t, expected.Reason, actual.Reason, "unexpected reason in condition %d", i)
					assert.Equal(t, expected.Message, actual.Message, "unexpected message in condition %d", i)
					if expected.LastTransitionTime.IsZero() {
						assert.Equal(t, expected.LastTransitionTime, actual.LastTransitionTime, "unexpected last transition time in condition %d", i)
					} else if actual.LastTransitionTime.IsZero() {
						t.Errorf("last probe time not set for condition %d", i)
					} else if actual.LastTransitionTime.Time.Before(startTime) || endTime.Before(actual.LastTransitionTime.Time) {
						t.Errorf("last probe time not within expected bounds for condition %d", i)
					}
					if expected.LastProbeTime.IsZero() {
						assert.Equal(t, expected.LastProbeTime, actual.LastProbeTime, "unexpected last probe time in condition %d", i)
					} else if actual.LastProbeTime.IsZero() {
						t.Errorf("last probe time not set for condition %d", i)
					} else if actual.LastProbeTime.Time.Before(startTime) || endTime.Before(actual.LastProbeTime.Time) {
						t.Errorf("last probe time not within expected bounds for condition %d", i)
					}
				}
			}
		})
	}
}

// TestFindClusterCondition tests the FindClusterCondition function.
func TestFindClusterCondition(t *testing.T) {
	cases := []struct {
		name                   string
		conditions             []cov1.ClusterCondition
		conditionType          cov1.ClusterConditionType
		expectedConditionIndex int
	}{
		{
			name:                   "no conditions",
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: -1,
		},
		{
			name: "only condition",
			conditions: []cov1.ClusterCondition{
				{Type: cov1.ClusterReady},
			},
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: 0,
		},
		{
			name: "first condition",
			conditions: []cov1.ClusterCondition{
				{Type: cov1.ClusterReady},
				{Type: cov1.ClusterInfraProvisioning},
			},
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: 0,
		},
		{
			name: "last condition",
			conditions: []cov1.ClusterCondition{
				{Type: cov1.ClusterInfraProvisioning},
				{Type: cov1.ClusterReady},
			},
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: 1,
		},
		{
			name: "middle condition",
			conditions: []cov1.ClusterCondition{
				{Type: cov1.ClusterInfraProvisioning},
				{Type: cov1.ClusterReady},
				{Type: cov1.ClusterInfraProvisioned},
			},
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: 1,
		},
		{
			name: "single non-matching condition",
			conditions: []cov1.ClusterCondition{
				{Type: cov1.ClusterInfraProvisioning},
			},
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: -1,
		},
		{
			name: "multiple non-matching conditions",
			conditions: []cov1.ClusterCondition{
				{Type: cov1.ClusterInfraProvisioning},
				{Type: cov1.ClusterInfraProvisioned},
				{Type: cov1.ClusterInfraProvisioningFailed},
			},
			conditionType:          cov1.ClusterReady,
			expectedConditionIndex: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := FindClusterCondition(tc.conditions, tc.conditionType)
			if tc.expectedConditionIndex < 0 {
				assert.Nil(t, actual, "expected to not find condition")
			} else {
				expected := &tc.conditions[tc.expectedConditionIndex]
				assert.Equal(t, expected, actual, "unexecpted condition found")
			}
		})
	}
}

func newNonControllingOwnerRef(owner metav1.Object, gvk schema.GroupVersionKind) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
	}
}

// TestGetObjectController tests the GetObjectController function.
func TestGetObjectController(t *testing.T) {
	cases := []struct {
		name               string
		ownerRefs          []metav1.OwnerReference
		controller         metav1.Object
		controllerGetError error
		expectedName       string
		expectedController metav1.Object
		expectedError      bool
	}{
		{
			name:       "no owner",
			controller: &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
		},
		{
			name: "no controlling owner",
			ownerRefs: []metav1.OwnerReference{
				newNonControllingOwnerRef(&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"}, clusterKind),
			},
			controller: &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
		},
		{
			name: "controller get error",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"}, clusterKind),
			},
			controllerGetError: fmt.Errorf("error getting controller"),
			expectedName:       "test-cluster",
			expectedError:      true,
		},
		{
			name: "controller get returns no controller",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"}, clusterKind),
			},
			expectedName: "test-cluster",
		},
		{
			name: "controller has other kind",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
					clusterKind.GroupVersion().WithKind("other-kind")),
			},
			controller: &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
		},
		{
			name: "controller has other group",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
					schema.GroupVersionKind{Group: "other-group", Version: clusterKind.Version, Kind: clusterKind.Kind}),
			},
			controller: &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
		},
		{
			name: "controller has other version",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
					schema.GroupVersionKind{Group: clusterKind.Group, Version: "other-version", Kind: clusterKind.Kind}),
			},
			controller: &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
		},
		{
			name: "controller has other UID",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"}, clusterKind),
			},
			controller:   &metav1.ObjectMeta{Name: "test-cluster", UID: "other-uid"},
			expectedName: "test-cluster",
		},
		{
			name: "controller found",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(&metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"}, clusterKind),
			},
			controller:         &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
			expectedName:       "test-cluster",
			expectedController: &metav1.ObjectMeta{Name: "test-cluster", UID: "test-uid"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &metav1.ObjectMeta{
				OwnerReferences: tc.ownerRefs,
			}

			getFunc := func(name string) (metav1.Object, error) {
				assert.Equal(t, tc.expectedName, name, "unexpected name to get controller")
				return tc.controller, tc.controllerGetError
			}

			actual, err := GetObjectController(obj, clusterKind, getFunc)
			assert.Equal(t, tc.expectedController, actual, "unexpected cluster")
			assert.Equal(t, tc.expectedError, err != nil, "unexpected error")
		})
	}
}

func TestJobLabelsForClusterController(t *testing.T) {
	cluster := &metav1.ObjectMeta{
		UID:  "test-uid",
		Name: "test-name",
	}
	expected := map[string]string{
		"clusteroperator.openshift.io/cluster": "test-name",
		"job-type":                             "test-job-type",
	}
	actual := JobLabelsForClusterController(cluster, "test-job-type")
	assert.Equal(t, expected, actual)
}

func TestAddLabels(t *testing.T) {
	cases := []struct {
		name       string
		existing   map[string]string
		additional map[string]string
		expected   map[string]string
	}{
		{
			name:     "none",
			expected: map[string]string{},
		},
		{
			name: "no existing",
			additional: map[string]string{
				"additional1": "value1",
				"additional2": "value2",
			},
			expected: map[string]string{
				"additional1": "value1",
				"additional2": "value2",
			},
		},
		{
			name: "no additional",
			existing: map[string]string{
				"existing1": "value1",
				"existing2": "value2",
			},
			expected: map[string]string{
				"existing1": "value1",
				"existing2": "value2",
			},
		},
		{
			name: "both",
			existing: map[string]string{
				"existing1": "value1",
				"existing2": "value2",
			},
			additional: map[string]string{
				"additional1": "value1",
				"additional2": "value2",
			},
			expected: map[string]string{
				"existing1":   "value1",
				"existing2":   "value2",
				"additional1": "value1",
				"additional2": "value2",
			},
		},
		{
			name: "overlapping",
			existing: map[string]string{
				"existing1": "value1",
				"existing2": "value2",
				"shared":    "existingValue",
			},
			additional: map[string]string{
				"additional1": "value1",
				"additional2": "value2",
				"shared":      "additionalValue",
			},
			expected: map[string]string{
				"existing1":   "value1",
				"existing2":   "value2",
				"additional1": "value1",
				"additional2": "value2",
				"shared":      "additionalValue",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &metav1.ObjectMeta{
				Labels: tc.existing,
			}
			AddLabels(obj, tc.additional)
			assert.Equal(t, tc.expected, obj.Labels)
		})
	}
}

func TestClusterDeploymentSpecFromCluster(t *testing.T) {
	cases := []struct {
		name            string
		providerConfig  string
		expectedVersion string
		expectedError   bool
	}{
		{
			name: "good",
			providerConfig: `
apiVersion: clusteroperator.openshift.io/v1alpha1
kind: AWSClusterProviderConfig
openshiftConfig:
  version:
    deploymentType: origin
    version: v3.10.0
    images:
      openshiftAnsibleImage: fake-openshift-ansible:canary
      openshiftAnsibleImagePullPolicy: Never
`,
			expectedVersion: "v3.10.0",
		},
		{
			name: "different type",
			providerConfig: `
apiVersion: clusteroperator.openshift.io/v1alpha1
kind: Machine
`,
			expectedError: true,
		},
		{
			name:           "missing provider config",
			providerConfig: "",
			expectedError:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &capiv1.Cluster{}
			if tc.providerConfig != "" {
				cluster.Spec.ProviderConfig.Value = &runtime.RawExtension{
					Raw: []byte(tc.providerConfig),
				}
			}
			clusterSpec, err := AWSClusterProviderConfigFromCluster(cluster)
			if tc.expectedError {
				assert.Error(t, err, "expected an error")
			} else {
				if !assert.NoError(t, err, "expected success") {
					return
				}
				assert.Equal(t, clusterSpec.OpenShiftConfig.Version.Version, tc.expectedVersion, "unexpected clusterVersionRef name")
			}
		})
	}
}

func TestClusterStatusFromClusterAPI(t *testing.T) {
	cases := []struct {
		name                          string
		providerStatus                string
		expectedClusterVersionRefName string
		expectedError                 bool
	}{
		{
			name: "good",
			providerStatus: `
apiVersion: clusteroperator.openshift.io/v1alpha1
kind: ClusterProviderStatus
clusterVersionRef:
  namespace: cluster-version-namespace
  name: cluster-version
`,
			expectedClusterVersionRefName: "cluster-version",
		},
		{
			name: "different type",
			providerStatus: `
apiVersion: clusteroperator.openshift.io/v1alpha1
kind: Machine
`,
			expectedError: true,
		},
		{
			name:           "missing provider status",
			providerStatus: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &capiv1.Cluster{}
			if tc.providerStatus != "" {
				cluster.Status.ProviderStatus = &runtime.RawExtension{
					Raw: []byte(tc.providerStatus),
				}
			}
			clusterStatus, err := ClusterProviderStatusFromCluster(cluster)
			if tc.expectedError {
				assert.Error(t, err, "expected an error")
			} else {
				if !assert.NoError(t, err, "expected success") {
					return
				}
				if tc.expectedClusterVersionRefName != "" {
					assert.Equal(t, clusterStatus.ClusterVersionRef.Name, tc.expectedClusterVersionRefName, "unexpected clusterVersionRef name")
				} else {
					assert.Nil(t, clusterStatus.ClusterVersionRef, "unexpected clusterVersionRef")
				}
			}
		})
	}
}

func TestClusterAPIProviderStatusFromClusterStatus(t *testing.T) {
	clusterStatus := &cov1.ClusterProviderStatus{
		ControlPlaneInstalled:    true,
		ProvisionedJobGeneration: 5,
	}
	providerStatus, err := EncodeClusterProviderStatus(clusterStatus)
	if !assert.NoError(t, err, "unexpected error converting to provider status") {
		return
	}
	t.Logf("provider status = %v", string(providerStatus.Raw))
	providerStatusData := map[string]interface{}{}
	err = json.Unmarshal(providerStatus.Raw, &providerStatusData)
	if !assert.NoError(t, err, "unexpected error unmarshalling json") {
		return
	}
	assert.Equal(t, "clusteroperator.openshift.io/v1alpha1", providerStatusData["apiVersion"], "unexpected apiVersion")
	assert.Equal(t, "ClusterProviderStatus", providerStatusData["kind"], "unexpected kind")
	assert.Equal(t, 5., providerStatusData["provisionedJobGeneration"], "unexpected provisionedJobGeneration")
	assert.Equal(t, true, providerStatusData["controlPlaneInstalled"], "unexpected controlPlaneInstalled")
}

func TestGetImage(t *testing.T) {
	cases := []struct {
		name             string
		clusterVersion   *cov1.ClusterVersion
		clusterSpec      *cov1.ClusterDeploymentSpec
		expectedErrorMsg string
	}{
		{
			name:           "single region cluster version",
			clusterVersion: newClusterVersion("origin-v3-10"),
			clusterSpec:    newClusterSpec(testRegion, defaultInstanceType),
		},
		{
			name:           "cluster has no AWS hardware",
			clusterVersion: newClusterVersion("origin-v3-10"),
			clusterSpec: func() *cov1.ClusterDeploymentSpec {
				cs := newClusterSpec(testRegion, defaultInstanceType)
				cs.Hardware.AWS = nil
				return cs
			}(),
			expectedErrorMsg: "no AWS hardware defined for cluster",
		},
		{
			name: "cluster version has no AWS images",
			clusterVersion: func() *cov1.ClusterVersion {
				cv := newClusterVersion("origin-v3-10")
				cv.Spec.VMImages.AWSImages = nil
				return cv
			}(),
			clusterSpec:      newClusterSpec(testRegion, defaultInstanceType),
			expectedErrorMsg: "no AWS images defined for cluster version",
		},
		{
			name:           "no matching region",
			clusterVersion: newClusterVersion("origin-v3-10"),
			clusterSpec: func() *cov1.ClusterDeploymentSpec {
				cs := newClusterSpec(testRegion, defaultInstanceType)
				cs.Hardware.AWS.Region = "us-west-notreal"
				return cs
			}(),
			expectedErrorMsg: "no AWS image defined for region",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img, err := getImage(tc.clusterSpec, tc.clusterVersion)
			if tc.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrorMsg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, testAMI, *img.AWSImage)
			}
		})
	}
}

func newClusterSpec(region, instanceType string) *cov1.ClusterDeploymentSpec {
	return &cov1.ClusterDeploymentSpec{
		Hardware: cov1.ClusterHardwareSpec{
			AWS: &cov1.AWSClusterSpec{
				Region: testRegion,
			},
		},
		DefaultHardwareSpec: &cov1.MachineSetHardwareSpec{
			AWS: &cov1.MachineSetAWSHardwareSpec{
				InstanceType: instanceType,
			},
		},
	}
}

func newClusterVersion(name string) *cov1.ClusterVersion {
	cv := &cov1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "testuid",
			Name:      name,
			Namespace: "testns",
		},
		Spec: cov1.ClusterVersionSpec{
			Images: cov1.ClusterVersionImages{
				ImageFormat: "openshift/origin-${component}:${version}",
			},
			VMImages: cov1.VMImages{
				AWSImages: &cov1.AWSVMImages{
					RegionAMIs: []cov1.AWSRegionAMIs{
						{
							Region: testRegion,
							AMI:    testAMI,
						},
					},
				},
			},
		},
	}
	return cv
}

func newMachineSpec(msSpec *cov1.MachineSetSpec) capiv1.MachineSpec {
	ms := capiv1.MachineSpec{}
	providerConfig, _ := MachineProviderConfigFromMachineSetSpec(msSpec)
	ms.ProviderConfig.Value = providerConfig
	return ms
}

func newMachineSetSpec(instanceType, vmImage string) *cov1.MachineSetSpec {
	msSpec := &cov1.MachineSetSpec{
		VMImage: cov1.VMImage{
			AWSImage: &vmImage,
		},
	}
	msSpec.Hardware = &cov1.MachineSetHardwareSpec{
		AWS: &cov1.MachineSetAWSHardwareSpec{
			InstanceType: instanceType,
		},
	}
	return msSpec
}

// TestApplyDefaultMachineSetHardwareSpec tests merging a default hardware spec with a specific spec from a
// machine set
func TestApplyDefaultMachineSetHardwareSpec(t *testing.T) {

	awsSpec := func(amiName, instanceType string) *cov1.MachineSetHardwareSpec {
		return &cov1.MachineSetHardwareSpec{
			AWS: &cov1.MachineSetAWSHardwareSpec{
				InstanceType: instanceType,
			},
		}
	}
	cases := []struct {
		name        string
		defaultSpec *cov1.MachineSetHardwareSpec
		specific    *cov1.MachineSetHardwareSpec
		expected    *cov1.MachineSetHardwareSpec
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
			specific:    &cov1.MachineSetHardwareSpec{},
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
		result, err := ApplyDefaultMachineSetHardwareSpec(tc.specific, tc.defaultSpec)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(result, tc.expected) {
			t.Errorf("%s: unexpected result. Expected: %v, Got: %v", tc.name, tc.expected, result)
		}
	}
}

func TestELBBasenameFromClusterID(t *testing.T) {
	cases := []struct {
		name     string
		basename string
		maxLen   int
		expected string
	}{
		{
			name:     "short",
			basename: "short",
			maxLen:   6,
			expected: "short",
		},
		{
			name:     "max len",
			basename: "maxlen",
			maxLen:   6,
			expected: "maxlen",
		},
		{
			name:     "long",
			basename: "toolong",
			maxLen:   6,
			expected: "toolon",
		},
		{
			name:     "trim on hyphen",
			basename: "abcd-1234",
			maxLen:   8,
			expected: "abc-1234",
		},
		{
			name:     "remove entire beginning",
			basename: "abcd-123456",
			maxLen:   6,
			expected: "123456",
		},
		{
			name:     "remove entire beginning would start at hyphen",
			basename: "abcd-123456",
			maxLen:   7,
			expected: "123456",
		},
		{
			name:     "trim off ending",
			basename: "abcd-123456",
			maxLen:   5,
			expected: "12345",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := trimForELBBasename(tc.basename, tc.maxLen)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestStringPtrsEqual(t *testing.T) {
	cases := []struct {
		name  string
		s1    *string
		s2    *string
		equal bool
	}{
		{
			name:  "both nil",
			s1:    nil,
			s2:    nil,
			equal: true,
		},
		{
			name:  "s1 nil",
			s1:    nil,
			s2:    stringPtr("a"),
			equal: false,
		},
		{
			name:  "s2 nil",
			s1:    stringPtr("a"),
			s2:    nil,
			equal: false,
		},
		{
			name:  "strings equal",
			s1:    stringPtr("aaa"),
			s2:    stringPtr("aaa"),
			equal: true,
		},
		{
			name:  "strings not equal",
			s1:    stringPtr("aaa"),
			s2:    stringPtr("bbb"),
			equal: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.equal, StringPtrsEqual(tc.s1, tc.s2))
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
