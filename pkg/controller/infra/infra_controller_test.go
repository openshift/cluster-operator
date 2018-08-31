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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"
	clusterapiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"

	"github.com/stretchr/testify/assert"
)

const (
	testNamespace   = "test-namespace"
	testClusterName = "test-cluster"
	testClusterUUID = types.UID("test-cluster-uuid")
)

// newTestController creates a test Controller for
// cluster-api resources with fake clients and informers.
func newTestController() (
	*Controller,
	cache.Store, // cluster store
	cache.Store, // jobs store
	*clientgofake.Clientset,
	*clusteroperatorclientset.Clientset,
	*clusterapiclientset.Clientset,

) {
	kubeClient := &clientgofake.Clientset{}
	kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
	clusterOperatorClient := &clusteroperatorclientset.Clientset{}
	clusterAPIClient := &clusterapiclientset.Clientset{}
	clusterAPIInformers := clusterapiinformers.NewSharedInformerFactory(clusterAPIClient, 0)

	controller := NewController(
		clusterAPIInformers.Cluster().V1alpha1().Clusters(),
		kubeInformers.Batch().V1().Jobs(),
		kubeClient,
		clusterOperatorClient,
		clusterAPIClient,
	)

	controller.clustersSynced = alwaysReady

	return controller,
		clusterAPIInformers.Cluster().V1alpha1().Clusters().Informer().GetStore(),
		kubeInformers.Batch().V1().Jobs().Informer().GetStore(),
		kubeClient,
		clusterOperatorClient,
		clusterAPIClient
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
func getKey(cluster metav1.Object, t *testing.T) string {
	key, err := cocontroller.KeyFunc(cluster)
	if err != nil {
		t.Errorf("Unexpected error getting key for Cluster %v: %v", cluster.GetName(), err)
		return ""
	}
	return key
}

func newCluster() *capiv1.Cluster {
	encodedClusterProviderConfigSpec := `
apiVersion: "clusteroperator.openshift.io/v1alpha1"
kind: "AWSClusterProviderConfig"
machineSets:
- nodeType: master
  infra: true
  size: 3
`
	cluster := &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterUUID,
			Name:      testClusterName,
			Namespace: testNamespace,
		},
		Spec: capiv1.ClusterSpec{
			ProviderConfig: capiv1.ProviderConfig{
				Value: &runtime.RawExtension{
					Raw: []byte(encodedClusterProviderConfigSpec),
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
		name               string
		useClusterOperator bool
		clusterName        string
		clusterNamespace   string
		expectedErr        bool
	}{
		{
			name:               "new cluster-api cluster creation",
			useClusterOperator: false,
			clusterName:        testClusterName,
			clusterNamespace:   testNamespace,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			infraController, clusterStore, _, _, _, _ := newTestController()

			cluster := newCluster()
			clusterStore.Add(cluster)

			err := infraController.syncHandler(getKey(cluster, t))
			if tc.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
