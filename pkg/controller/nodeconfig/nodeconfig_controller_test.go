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

package nodeconfig

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	co "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller/fake"

	gomock "github.com/golang/mock/gomock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func TestStrategyDoesOwnerNeedProcessing(t *testing.T) {
	cases := []struct {
		name                          string
		controlPlaneInstalled         bool
		machineSetGeneration          int64
		nodeConfigInstalledGeneration int64
		needsProcessing               bool
	}{
		{
			name: "control plane not installed",
			controlPlaneInstalled: false,
			needsProcessing:       false,
		},
		{
			name: "node config not installed",
			controlPlaneInstalled: true,
			machineSetGeneration:  1,
			needsProcessing:       true,
		},
		{
			name: "node config installed",
			controlPlaneInstalled:         true,
			machineSetGeneration:          1,
			nodeConfigInstalledGeneration: 1,
			needsProcessing:               false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			clusterLister := fake.NewMockClusterLister(mockCtrl)
			clusterNamespaceLister := fake.NewMockClusterNamespaceLister(mockCtrl)

			ncController := Controller{
				clustersLister: clusterLister,
				logger:         log.WithField("test", fmt.Sprintf("TestStrategyDoesOwnerNeedProcessing/%s", tc.name)),
			}
			strat := jobSyncStrategy{controller: &ncController}

			namespace := "test-namespace"
			clusterName := "test-cluster"
			clusterUID := types.UID("test-cluster-UID")
			cluster := &co.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      clusterName,
					UID:       clusterUID,
				},
				Status: co.ClusterStatus{
					ControlPlaneInstalled: tc.controlPlaneInstalled,
				},
			}
			ownerRef := metav1.NewControllerRef(cluster, co.SchemeGroupVersion.WithKind("Cluster"))
			ms := &co.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       namespace,
					OwnerReferences: []metav1.OwnerReference{*ownerRef},
					Generation:      tc.machineSetGeneration,
				},
			}
			if tc.nodeConfigInstalledGeneration > 0 {
				ms.Status.NodeConfigInstalledJobGeneration = tc.nodeConfigInstalledGeneration
				ms.Status.NodeConfigInstalled = true
			}

			clusterLister.EXPECT().Clusters(namespace).
				Return(clusterNamespaceLister)
			clusterNamespaceLister.EXPECT().Get(clusterName).
				Return(cluster, nil)

			assert.Equal(t, tc.needsProcessing, strat.DoesOwnerNeedProcessing(ms), "unexpected DoesOwnerNeedProcessing")
		})
	}
}
