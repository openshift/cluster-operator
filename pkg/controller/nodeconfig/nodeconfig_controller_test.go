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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func TestStrategyReadyToInstall(t *testing.T) {
	cases := []struct {
		name                                    string
		controlPlaneInstalled                   bool
		clusterGeneration                       int64
		machineSetGeneration                    int64
		nodeConfigInstalledClusterGeneration    int64
		nodeConfigInstalledMachineSetGeneration int64
		needsProcessing                         bool
	}{
		{
			name: "control plane not installed",
			controlPlaneInstalled: false,
			needsProcessing:       false,
		},
		{
			name: "node config not installed",
			controlPlaneInstalled: true,
			clusterGeneration:     1,
			machineSetGeneration:  1,
			needsProcessing:       true,
		},
		{
			name: "node config installed",
			controlPlaneInstalled:                   true,
			clusterGeneration:                       1,
			machineSetGeneration:                    1,
			nodeConfigInstalledClusterGeneration:    1,
			nodeConfigInstalledMachineSetGeneration: 1,
			needsProcessing:                         false,
		},
		{
			name: "updated cluster",
			controlPlaneInstalled:                   true,
			clusterGeneration:                       2,
			machineSetGeneration:                    1,
			nodeConfigInstalledClusterGeneration:    1,
			nodeConfigInstalledMachineSetGeneration: 1,
			needsProcessing:                         true,
		},
		{
			name: "updated machine set",
			controlPlaneInstalled:                   true,
			clusterGeneration:                       1,
			machineSetGeneration:                    2,
			nodeConfigInstalledClusterGeneration:    1,
			nodeConfigInstalledMachineSetGeneration: 1,
			needsProcessing:                         true,
		},
		{
			name: "updated cluster and machine set",
			controlPlaneInstalled:                   true,
			clusterGeneration:                       2,
			machineSetGeneration:                    2,
			nodeConfigInstalledClusterGeneration:    1,
			nodeConfigInstalledMachineSetGeneration: 1,
			needsProcessing:                         true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			strat := &installStrategy{}

			cluster := &cov1.CombinedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Generation: tc.clusterGeneration,
				},
				ClusterProviderStatus: &cov1.ClusterProviderStatus{
					ControlPlaneInstalled:                      tc.controlPlaneInstalled,
					NodeConfigInstalledJobClusterGeneration:    tc.nodeConfigInstalledClusterGeneration,
					NodeConfigInstalledJobMachineSetGeneration: tc.nodeConfigInstalledMachineSetGeneration,
				},
			}

			ms := &capiv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Generation: tc.machineSetGeneration,
				},
			}

			assert.Equal(t, tc.needsProcessing, strat.ReadyToInstall(cluster, ms), "unexpected ReadToInstall")
		})
	}
}
