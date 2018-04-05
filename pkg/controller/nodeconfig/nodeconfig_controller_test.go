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

	co "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func TestStrategyDoesOwnerNeedProcessing(t *testing.T) {
	cases := []struct {
		name                          string
		machineSetInstalled           bool
		machineSetGeneration          int64
		nodeConfigInstalledGeneration int64
		needsProcessing               bool
	}{
		{
			name:                "control plane not installed",
			machineSetInstalled: false,
			needsProcessing:     false,
		},
		{
			name:                 "node config not installed",
			machineSetInstalled:  true,
			machineSetGeneration: 1,
			needsProcessing:      true,
		},
		{
			name:                          "node config installed",
			machineSetInstalled:           true,
			machineSetGeneration:          1,
			nodeConfigInstalledGeneration: 1,
			needsProcessing:               false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ncController := Controller{
				logger: log.WithField("test", fmt.Sprintf("TestStrategyDoesOwnerNeedProcessing/%s", tc.name)),
			}
			strat := jobSyncStrategy{controller: &ncController}
			ms := &co.MachineSet{}
			ms.Generation = tc.machineSetGeneration
			ms.Status.Installed = tc.machineSetInstalled
			if tc.nodeConfigInstalledGeneration > 0 {
				ms.Status.NodeConfigInstalledJobGeneration = tc.nodeConfigInstalledGeneration
				ms.Status.NodeConfigInstalled = true
			}
			assert.Equal(t, tc.needsProcessing, strat.DoesOwnerNeedProcessing(ms), "unexpected DoesOwnerNeedProcessing")
		})
	}
}
