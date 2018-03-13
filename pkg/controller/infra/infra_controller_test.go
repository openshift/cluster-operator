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

package infra

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	"github.com/openshift/cluster-operator/pkg/controller"

	"github.com/stretchr/testify/assert"
)

const (
	testNamespace   = "test-namespace"
	testClusterName = "test-cluster"
	testClusterUUID = types.UID("test-cluster-uuid")
)

// newTestController creates a test Controller with fake
// clients and informers.
func newTestController() (
	*Controller,
	cache.Store, // cluster store
	cache.Store, // jobs store
	*clientgofake.Clientset,
	*clusteroperatorclientset.Clientset,
) {
	kubeClient := &clientgofake.Clientset{}
	kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
	clusterOperatorClient := &clusteroperatorclientset.Clientset{}
	clusterOperatorInformers := informers.NewSharedInformerFactory(clusterOperatorClient, 0)

	controller := NewController(
		clusterOperatorInformers.Clusteroperator().V1alpha1().Clusters(),
		kubeInformers.Batch().V1().Jobs(),
		kubeClient,
		clusterOperatorClient,
	)

	controller.clustersSynced = alwaysReady

	return controller,
		clusterOperatorInformers.Clusteroperator().V1alpha1().Clusters().Informer().GetStore(),
		kubeInformers.Batch().V1().Jobs().Informer().GetStore(),
		kubeClient,
		clusterOperatorClient
}

// alwaysReady is a function that can be used as a sync function that will
// always indicate that the lister has been synced.
var alwaysReady = func() bool { return true }

type fakeAnsibleRunner struct {
	lastNamespace   string
	lastClusterName string
	lastJobPrefix   string
	lastPlaybook    string
}

func (r *fakeAnsibleRunner) RunPlaybook(namespace, clusterName, jobPrefix, playbook, inventory, vars string) error {
	// Record what we were called with for assertions:
	r.lastNamespace = namespace
	r.lastClusterName = clusterName
	r.lastJobPrefix = jobPrefix
	r.lastPlaybook = playbook
	return nil
}

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

func newCluster() *clusteroperator.Cluster {
	cluster := &clusteroperator.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterUUID,
			Name:      testClusterName,
			Namespace: testNamespace,
		},
		Spec: clusteroperator.ClusterSpec{
			MachineSets: []clusteroperator.ClusterMachineSet{
				{
					Name: "master",
					MachineSetConfig: clusteroperator.MachineSetConfig{
						NodeType: clusteroperator.NodeTypeMaster,
						Infra:    true,
						Size:     3,
					},
				},
			},
		},
	}
	return cluster
}

// TestController performs basic unit tests on the infra controller to ensure it
// interacts with the AnsibleRunner correctly.
func TestController(t *testing.T) {
	cases := []struct {
		name             string
		clusterName      string
		clusterNamespace string
		expectedErr      bool
	}{
		{
			name:             "new cluster creation",
			clusterName:      testClusterName,
			clusterNamespace: testNamespace,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, clusterStore, _, _, _ := newTestController()

			cluster := newCluster()
			clusterStore.Add(cluster)

			err := controller.syncHandler(getKey(cluster, t))
			if tc.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
