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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
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
	conditionType clusteroperator.ClusterConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	lastTransitionTime metav1.Time,
	lastProbeTime metav1.Time,
) clusteroperator.ClusterCondition {
	return clusteroperator.ClusterCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: lastTransitionTime,
		LastProbeTime:      lastProbeTime,
	}
}

// TestSetClusterCondtion tests the SetClusterCondtion function.
func TestSetClusterCondtion(t *testing.T) {
	cases := []struct {
		name               string
		existingConditions []clusteroperator.ClusterCondition
		conditionType      clusteroperator.ClusterConditionType
		status             corev1.ConditionStatus
		reason             string
		message            string
		updateCondition    bool
		expectedConditions []clusteroperator.ClusterCondition
		expectedOldReason  string
		expectedOldMessage string
	}{
		{
			name:          "new condition",
			conditionType: clusteroperator.ClusterReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "reason", "message", metav1.Now(), metav1.Now()),
			},
		},
		{
			name: "new condition with existing conditions",
			existingConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterInfraProvisioning, corev1.ConditionTrue, "other reason", "other message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: clusteroperator.ClusterReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterInfraProvisioning, corev1.ConditionTrue, "other reason", "other message", metav1.Time{}, metav1.Time{}),
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "reason", "message", metav1.Now(), metav1.Now()),
			},
		},
		{
			name:          "false condition not created",
			conditionType: clusteroperator.ClusterReady,
			status:        corev1.ConditionFalse,
			reason:        "reason",
			message:       "message",
		},
		{
			name: "condition not updated",
			existingConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: clusteroperator.ClusterReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
		{
			name: "condition status changed",
			existingConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: clusteroperator.ClusterReady,
			status:        corev1.ConditionFalse,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionFalse, "reason", "message", metav1.Now(), metav1.Now()),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
		{
			name: "condition changed due to update check",
			existingConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType:   clusteroperator.ClusterReady,
			status:          corev1.ConditionTrue,
			reason:          "reason",
			message:         "message",
			updateCondition: true,
			expectedConditions: []clusteroperator.ClusterCondition{
				newClusterCondition(clusteroperator.ClusterReady, corev1.ConditionTrue, "reason", "message", metav1.Time{}, metav1.Now()),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &clusteroperator.Cluster{
				Status: clusteroperator.ClusterStatus{
					Conditions: tc.existingConditions,
				},
			}
			updateCheck := func(oldReason, oldMessage, newReason, newMessage string) bool {
				assert.Equal(t, tc.expectedOldReason, oldReason, "unexpected old reason passed to update condition check")
				assert.Equal(t, tc.expectedOldMessage, oldMessage, "unexpected old message passed to update condition check")
				assert.Equal(t, tc.reason, newReason, "unexpected new reason passed to update condition check")
				assert.Equal(t, tc.message, newMessage, "unexpected new message passed to update condition check")
				return tc.updateCondition
			}
			startTime := time.Now()
			SetClusterCondition(
				cluster,
				tc.conditionType,
				tc.status,
				tc.reason,
				tc.message,
				updateCheck,
			)
			endTime := time.Now()
			actualConditions := cluster.Status.Conditions
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
		conditions             []clusteroperator.ClusterCondition
		conditionType          clusteroperator.ClusterConditionType
		expectedConditionIndex int
	}{
		{
			name:                   "no conditions",
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: -1,
		},
		{
			name: "only condition",
			conditions: []clusteroperator.ClusterCondition{
				{Type: clusteroperator.ClusterReady},
			},
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: 0,
		},
		{
			name: "first condition",
			conditions: []clusteroperator.ClusterCondition{
				{Type: clusteroperator.ClusterReady},
				{Type: clusteroperator.ClusterInfraProvisioning},
			},
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: 0,
		},
		{
			name: "last condition",
			conditions: []clusteroperator.ClusterCondition{
				{Type: clusteroperator.ClusterInfraProvisioning},
				{Type: clusteroperator.ClusterReady},
			},
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: 1,
		},
		{
			name: "middle condition",
			conditions: []clusteroperator.ClusterCondition{
				{Type: clusteroperator.ClusterInfraProvisioning},
				{Type: clusteroperator.ClusterReady},
				{Type: clusteroperator.ClusterInfraProvisioned},
			},
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: 1,
		},
		{
			name: "single non-matching condition",
			conditions: []clusteroperator.ClusterCondition{
				{Type: clusteroperator.ClusterInfraProvisioning},
			},
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: -1,
		},
		{
			name: "multiple non-matching conditions",
			conditions: []clusteroperator.ClusterCondition{
				{Type: clusteroperator.ClusterInfraProvisioning},
				{Type: clusteroperator.ClusterInfraProvisioned},
				{Type: clusteroperator.ClusterInfraProvisioningFailed},
			},
			conditionType:          clusteroperator.ClusterReady,
			expectedConditionIndex: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &clusteroperator.Cluster{
				Status: clusteroperator.ClusterStatus{
					Conditions: tc.conditions,
				},
			}
			actual := FindClusterCondition(cluster, tc.conditionType)
			if tc.expectedConditionIndex < 0 {
				assert.Nil(t, actual, "expected to not find condition")
			} else {
				expected := &tc.conditions[tc.expectedConditionIndex]
				assert.Equal(t, expected, actual, "unexecpted condition found")
			}
		})
	}
}

func newMachineSetCondition(
	conditionType clusteroperator.MachineSetConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	lastTransitionTime metav1.Time,
	lastProbeTime metav1.Time,
) clusteroperator.MachineSetCondition {
	return clusteroperator.MachineSetCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: lastTransitionTime,
		LastProbeTime:      lastProbeTime,
	}
}

// TestSetMachineSetCondtion tests the SetMachineSetCondtion function.
func TestSetMachineSetCondtion(t *testing.T) {
	cases := []struct {
		name               string
		existingConditions []clusteroperator.MachineSetCondition
		conditionType      clusteroperator.MachineSetConditionType
		status             corev1.ConditionStatus
		reason             string
		message            string
		updateCondition    bool
		expectedConditions []clusteroperator.MachineSetCondition
		expectedOldReason  string
		expectedOldMessage string
	}{
		{
			name:          "new condition",
			conditionType: clusteroperator.MachineSetReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "reason", "message", metav1.Now(), metav1.Now()),
			},
		},
		{
			name: "new condition with existing conditions",
			existingConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetHardwareProvisioning, corev1.ConditionTrue, "other reason", "other message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: clusteroperator.MachineSetReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetHardwareProvisioning, corev1.ConditionTrue, "other reason", "other message", metav1.Time{}, metav1.Time{}),
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "reason", "message", metav1.Now(), metav1.Now()),
			},
		},
		{
			name:          "false condition not created",
			conditionType: clusteroperator.MachineSetReady,
			status:        corev1.ConditionFalse,
			reason:        "reason",
			message:       "message",
		},
		{
			name: "condition not updated",
			existingConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: clusteroperator.MachineSetReady,
			status:        corev1.ConditionTrue,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
		{
			name: "condition status changed",
			existingConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType: clusteroperator.MachineSetReady,
			status:        corev1.ConditionFalse,
			reason:        "reason",
			message:       "message",
			expectedConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionFalse, "reason", "message", metav1.Now(), metav1.Now()),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
		{
			name: "condition changed due to update check",
			existingConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "old reason", "old message", metav1.Time{}, metav1.Time{}),
			},
			conditionType:   clusteroperator.MachineSetReady,
			status:          corev1.ConditionTrue,
			reason:          "reason",
			message:         "message",
			updateCondition: true,
			expectedConditions: []clusteroperator.MachineSetCondition{
				newMachineSetCondition(clusteroperator.MachineSetReady, corev1.ConditionTrue, "reason", "message", metav1.Time{}, metav1.Now()),
			},
			expectedOldReason:  "old reason",
			expectedOldMessage: "old message",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machineSet := &clusteroperator.MachineSet{
				Status: clusteroperator.MachineSetStatus{
					Conditions: tc.existingConditions,
				},
			}
			updateCheck := func(oldReason, oldMessage, newReason, newMessage string) bool {
				assert.Equal(t, tc.expectedOldReason, oldReason, "unexpected old reason passed to update condition check")
				assert.Equal(t, tc.expectedOldMessage, oldMessage, "unexpected old message passed to update condition check")
				assert.Equal(t, tc.reason, newReason, "unexpected new reason passed to update condition check")
				assert.Equal(t, tc.message, newMessage, "unexpected new message passed to update condition check")
				return tc.updateCondition
			}
			startTime := time.Now()
			SetMachineSetCondition(
				machineSet,
				tc.conditionType,
				tc.status,
				tc.reason,
				tc.message,
				updateCheck,
			)
			endTime := time.Now()
			actualConditions := machineSet.Status.Conditions
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

// TestFindMachineSetCondition tests the FindMachineSetCondition function.
func TestFindMachineSetCondition(t *testing.T) {
	cases := []struct {
		name                   string
		conditions             []clusteroperator.MachineSetCondition
		conditionType          clusteroperator.MachineSetConditionType
		expectedConditionIndex int
	}{
		{
			name:                   "no conditions",
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: -1,
		},
		{
			name: "only condition",
			conditions: []clusteroperator.MachineSetCondition{
				{Type: clusteroperator.MachineSetReady},
			},
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: 0,
		},
		{
			name: "first condition",
			conditions: []clusteroperator.MachineSetCondition{
				{Type: clusteroperator.MachineSetReady},
				{Type: clusteroperator.MachineSetHardwareProvisioning},
			},
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: 0,
		},
		{
			name: "last condition",
			conditions: []clusteroperator.MachineSetCondition{
				{Type: clusteroperator.MachineSetHardwareProvisioning},
				{Type: clusteroperator.MachineSetReady},
			},
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: 1,
		},
		{
			name: "middle condition",
			conditions: []clusteroperator.MachineSetCondition{
				{Type: clusteroperator.MachineSetHardwareProvisioning},
				{Type: clusteroperator.MachineSetReady},
				{Type: clusteroperator.MachineSetHardwareProvisioned},
			},
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: 1,
		},
		{
			name: "single non-matching condition",
			conditions: []clusteroperator.MachineSetCondition{
				{Type: clusteroperator.MachineSetHardwareProvisioning},
			},
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: -1,
		},
		{
			name: "multiple non-matching conditions",
			conditions: []clusteroperator.MachineSetCondition{
				{Type: clusteroperator.MachineSetHardwareProvisioning},
				{Type: clusteroperator.MachineSetHardwareProvisioned},
				{Type: clusteroperator.MachineSetHardwareProvisioningFailed},
			},
			conditionType:          clusteroperator.MachineSetReady,
			expectedConditionIndex: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machineSet := &clusteroperator.MachineSet{
				Status: clusteroperator.MachineSetStatus{
					Conditions: tc.conditions,
				},
			}
			actual := FindMachineSetCondition(machineSet, tc.conditionType)
			if tc.expectedConditionIndex < 0 {
				assert.Nil(t, actual, "expected to not find condition")
			} else {
				expected := &tc.conditions[tc.expectedConditionIndex]
				assert.Equal(t, expected, actual, "unexecpted condition found")
			}
		})
	}
}

func testCluster(namespace, name string, uid types.UID) *clusteroperator.Cluster {
	return &clusteroperator.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       uid,
		},
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

// TestClusterForMachineSet tests the ClusterForMachineSet function.
func TestClusterForMachineSet(t *testing.T) {
	cases := []struct {
		name            string
		namespace       string
		ownerRefs       []metav1.OwnerReference
		clusters        []*clusteroperator.Cluster
		expectedCluster *clusteroperator.Cluster
		expectedError   bool
	}{
		{
			name:      "no owner",
			namespace: "test-namespace",
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "test-uid"),
			},
		},
		{
			name:      "no controlling owner",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				newNonControllingOwnerRef(testCluster("test-namespace", "test-cluster", "test-uid"), clusterKind),
			},
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "test-uid"),
			},
		},
		{
			name:      "controller not found",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(testCluster("test-namespace", "test-cluster", "test-uid"), clusterKind),
			},
			expectedError: true,
		},
		{
			name:      "controller has other kind",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					testCluster("test-namespace", "test-cluster", "test-uid"),
					clusterKind.GroupVersion().WithKind("other-kind")),
			},
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "test-uid"),
			},
		},
		{
			name:      "controller has other group",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					testCluster("test-namespace", "test-cluster", "test-uid"),
					schema.GroupVersionKind{Group: "other-group", Version: clusterKind.Version, Kind: clusterKind.Kind}),
			},
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "test-uid"),
			},
		},
		{
			name:      "controller has other version",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					testCluster("test-namespace", "test-cluster", "test-uid"),
					schema.GroupVersionKind{Group: clusterKind.Group, Version: "other-version", Kind: clusterKind.Kind}),
			},
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "test-uid"),
			},
		},
		{
			name:      "controller has other UID",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(testCluster("test-namespace", "test-cluster", "test-uid"), clusterKind),
			},
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "other-uid"),
			},
		},
		{
			name:      "controller found",
			namespace: "test-namespace",
			ownerRefs: []metav1.OwnerReference{
				*metav1.NewControllerRef(testCluster("test-namespace", "test-cluster", "test-uid"), clusterKind),
			},
			clusters: []*clusteroperator.Cluster{
				testCluster("test-namespace", "test-cluster", "test-uid"),
			},
			expectedCluster: testCluster("test-namespace", "test-cluster", "test-uid"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &clusteroperatorclientset.Clientset{}
			informers := informers.NewSharedInformerFactory(client, 0)
			store := informers.Clusteroperator().V1alpha1().Clusters().Informer().GetStore()
			lister := informers.Clusteroperator().V1alpha1().Clusters().Lister()

			machineSet := &clusteroperator.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       tc.namespace,
					OwnerReferences: tc.ownerRefs,
				},
			}

			for _, cluster := range tc.clusters {
				store.Add(cluster)
			}

			actual, err := ClusterForMachineSet(machineSet, lister)
			assert.Equal(t, tc.expectedCluster, actual, "unexpected cluster")
			assert.Equal(t, tc.expectedError, err != nil, "unexpected error")
		})
	}
}
