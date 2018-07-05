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

package integration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	// avoid error `clusteroperator/v1alpha1 is not enabled`
	_ "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/install"

	servertesting "github.com/openshift/cluster-operator/cmd/cluster-operator-apiserver/app/testing"
	clustopv1alpha1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	clustopinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/controller/awselb"
	clusterdeploymentcontroller "github.com/openshift/cluster-operator/pkg/controller/clusterdeployment"
	componentscontroller "github.com/openshift/cluster-operator/pkg/controller/components"
	deployclusterapicontroller "github.com/openshift/cluster-operator/pkg/controller/deployclusterapi"
	infracontroller "github.com/openshift/cluster-operator/pkg/controller/infra"
	mastercontroller "github.com/openshift/cluster-operator/pkg/controller/master"
	nodeconfigcontroller "github.com/openshift/cluster-operator/pkg/controller/nodeconfig"
	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capifakeclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"
)

const (
	testNamespace          = "test-namespace"
	testClusterName        = "test-cluster"
	testClusterVersionName = "v3-9"
)

func testMachineSet(name string, replicas int32, roles []capicommon.MachineRole, infra bool) *capiv1alpha1.MachineSet {
	return &capiv1alpha1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      name,
			Labels: map[string]string{
				clustopv1alpha1.ClusterNameLabel: testClusterName,
			},
		},
		Spec: capiv1alpha1.MachineSetSpec{
			Replicas: func(n int32) *int32 { return &n }(3),
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"set": name,
				},
			},
			Template: capiv1alpha1.MachineTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"set": name,
					},
				},
				Spec: capiv1alpha1.MachineSpec{
					Roles: roles,
					ProviderConfig: capiv1alpha1.ProviderConfig{
						Value: func() *runtime.RawExtension {
							r, _ := controller.MachineProviderConfigFromMachineSetSpec(&clustopv1alpha1.MachineSetSpec{
								MachineSetConfig: clustopv1alpha1.MachineSetConfig{
									Infra: infra,
								},
							})
							return r
						}(),
					},
				},
			},
		},
	}
}

// TestClusterCreate tests creating a cluster-api cluster.
func TestClusterCreate(t *testing.T) {
	cases := []struct {
		name        string
		machineSets []clustopv1alpha1.ClusterMachineSet
	}{
		{
			name: "no compute machine sets",
			machineSets: []clustopv1alpha1.ClusterMachineSet{
				{
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     3,
						NodeType: clustopv1alpha1.NodeTypeMaster,
						Infra:    true,
					},
				},
			},
		},
		{
			name: "single compute machine sets",
			machineSets: []clustopv1alpha1.ClusterMachineSet{
				{
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     3,
						NodeType: clustopv1alpha1.NodeTypeMaster,
						Infra:    true,
					},
				},
				{
					ShortName: "first",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     5,
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
					},
				},
			},
		},
		{
			name: "multiple compute machine sets",
			machineSets: []clustopv1alpha1.ClusterMachineSet{
				{
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     3,
						NodeType: clustopv1alpha1.NodeTypeMaster,
						Infra:    true,
					},
				},
				{
					ShortName: "first",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     5,
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
					},
				},
				{
					ShortName: "second",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     6,
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
					},
				},
				{
					ShortName: "3rd",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						Size:     3,
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kubeClient, _, clustopClient, capiClient, _, tearDown := startServerAndControllers(t)
			defer tearDown()

			clusterVersion := &clustopv1alpha1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterVersionName,
				},
				Spec: clustopv1alpha1.ClusterVersionSpec{
					Images: clustopv1alpha1.ClusterVersionImages{
						ImageFormat:                     "openshift/origin-${component}:${version}",
						OpenshiftAnsibleImage:           func(s string) *string { return &s }("test-ansible-image"),
						OpenshiftAnsibleImagePullPolicy: func(p kapi.PullPolicy) *kapi.PullPolicy { return &p }(kapi.PullNever),
					},
					VMImages: clustopv1alpha1.VMImages{
						AWSImages: &clustopv1alpha1.AWSVMImages{
							RegionAMIs: []clustopv1alpha1.AWSRegionAMIs{
								{
									Region: "us-east-1",
									AMI:    "computeAMI_ID",
								},
							},
						},
					},
					DeploymentType: clustopv1alpha1.ClusterDeploymentTypeOrigin,
					Version:        "v3.7.0",
				},
			}
			clustopClient.ClusteroperatorV1alpha1().ClusterVersions(testNamespace).Create(clusterVersion)

			clusterDeploymentSpec := &clustopv1alpha1.ClusterDeploymentSpec{
				ClusterID: testClusterName + "-abcde",
				ClusterVersionRef: clustopv1alpha1.ClusterVersionReference{
					Namespace: clusterVersion.Namespace,
					Name:      clusterVersion.Name,
				},
				Hardware: clustopv1alpha1.ClusterHardwareSpec{
					AWS: &clustopv1alpha1.AWSClusterSpec{
						Region: "us-east-1",
					},
				},
				DefaultHardwareSpec: &clustopv1alpha1.MachineSetHardwareSpec{
					AWS: &clustopv1alpha1.MachineSetAWSHardwareSpec{
						InstanceType: "instance-type",
					},
				},
				MachineSets: tc.machineSets,
			}
			cd := &clustopv1alpha1.ClusterDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterName,
				},
				Spec: *clusterDeploymentSpec,
			}
			cd, err := clustopClient.ClusteroperatorV1alpha1().ClusterDeployments(testNamespace).Create(cd)
			if !assert.NoError(t, err, "could not create the cluster deployment") {
				return
			}

			if err := waitForClusterToExist(capiClient, testNamespace, cd.Spec.ClusterID); err != nil {
				t.Fatalf("error waiting for Cluster to exist: %v", err)
			}

			cluster, err := capiClient.ClusterV1alpha1().Clusters(testNamespace).Get(cd.Spec.ClusterID, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error looking up cluster: %v", err)
			}

			if !completeInfraProvision(t, kubeClient, capiClient, cluster) {
				return
			}

			if err := waitForMachineSetToExist(capiClient, testNamespace, fmt.Sprintf("%s-%s", cd.Spec.ClusterID, "master")); err != nil {
				t.Fatalf("error waiting for master MachineSet to exist: %v", err)
			}

			if !completeControlPlaneInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeComponentsInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeNodeConfigInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeDeployClusterAPIInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if err := clustopClient.ClusteroperatorV1alpha1().ClusterDeployments(testNamespace).Delete(cd.Name, &metav1.DeleteOptions{}); err != nil {
				t.Fatalf("could not delete cluster deployment: %v", err)
			}

			if !completeInfraDeprovision(t, kubeClient, capiClient, cluster) {
				return
			}

			if err := waitForClusterToNotExist(capiClient, cluster.Namespace, cluster.Name); err != nil {
				t.Fatalf("cluster not removed: %v", err)
			}
		})
	}
}

// startServerAndControllers creates new test controllers injected with
// fake clients and returns:
//
// - a fake kubernetes core api client
// - a watch of kube resources
// - a cluster-operator api client
// - a cluster-api api client
// - a fake remote cluster-api api client
// - a function for shutting down the controller and API server
//
// If there is an error, startServerAndControllers calls 'Fatal' on the
// injected testing.T.
func startServerAndControllers(t *testing.T) (
	*kubefake.Clientset,
	watch.Interface,
	clustopclientset.Interface,
	capiclientset.Interface,
	*capifakeclientset.Clientset,
	func()) {

	// create a fake kube client
	fakePtr := clientgotesting.Fake{}
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	kubefake.AddToScheme(scheme)
	objectTracker := clientgotesting.NewObjectTracker(scheme, codecs.UniversalDecoder())
	kubeWatch := watch.NewRaceFreeFake()
	// Add a reactor for sending watch events when a job is modified
	objectReaction := clientgotesting.ObjectReaction(objectTracker)
	fakePtr.AddReactor("*", "jobs", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		var deletedObj runtime.Object
		if action, ok := action.(clientgotesting.DeleteActionImpl); ok {
			deletedObj, _ = objectTracker.Get(action.GetResource(), action.GetNamespace(), action.GetName())
		}
		handled, obj, err := objectReaction(action)
		switch action.(type) {
		case clientgotesting.CreateActionImpl:
			kubeWatch.Add(obj)
		case clientgotesting.UpdateActionImpl:
			kubeWatch.Modify(obj)
		case clientgotesting.DeleteActionImpl:
			if deletedObj != nil {
				kubeWatch.Delete(deletedObj)
			}
		}
		return handled, obj, err
	})
	fakePtr.AddWatchReactor("*", clientgotesting.DefaultWatchReactor(kubeWatch, nil))
	// Create actual fake kube client
	fakeKubeClient := &kubefake.Clientset{Fake: fakePtr}

	// start the cluster-operator api server
	apiServerClientConfig, shutdownServer := servertesting.StartTestServerOrDie(t)

	// create a cluster-operator client
	clustopClient, err := clustopclientset.NewForConfig(apiServerClientConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// create a cluster-api client
	capiClient, err := capiclientset.NewForConfig(apiServerClientConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fakeCAPIClient := &capifakeclientset.Clientset{}

	// create informers
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 10*time.Second)
	batchSharedInformers := kubeInformerFactory.Batch().V1()
	clustopInformerFactory := clustopinformers.NewSharedInformerFactory(clustopClient, 10*time.Second)
	clustopSharedInformers := clustopInformerFactory.Clusteroperator().V1alpha1()
	capiInformerFactory := capiinformers.NewSharedInformerFactory(capiClient, 10*time.Second)
	capiSharedInformers := capiInformerFactory.Cluster().V1alpha1()

	// create controllers
	stopCh := make(chan struct{})
	t.Log("controller start")
	// Note that controllers must be created prior to starting the informers.
	// Otherwise, the controllers will not get the initial sync from the
	// informer and will time out waiting to sync.
	runControllers := []func(){
		// clusterdeployment
		func() func() {
			controller := clusterdeploymentcontroller.NewController(
				clustopSharedInformers.ClusterDeployments(),
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				clustopSharedInformers.ClusterVersions(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// infra
		func() func() {
			controller := infracontroller.NewController(
				capiSharedInformers.Clusters(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// master
		func() func() {
			controller := mastercontroller.NewController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// components
		func() func() {
			controller := componentscontroller.NewController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// nodeconfig
		func() func() {
			controller := nodeconfigcontroller.NewController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// deployclusterapi
		func() func() {
			controller := deployclusterapicontroller.NewController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// awselb
		func() func() {
			controller := awselb.NewController(
				capiSharedInformers.Machines(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
	}
	var wg sync.WaitGroup
	wg.Add(len(runControllers))
	for _, run := range runControllers {
		go func(r func()) {
			defer wg.Done()
			r()
		}(run)
	}

	t.Log("informers start")
	kubeInformerFactory.Start(stopCh)
	clustopInformerFactory.Start(stopCh)
	capiInformerFactory.Start(stopCh)

	shutdown := func() {
		// Shut down controller
		close(stopCh)
		// Wait for all controller to stop
		wg.Wait()
		// Shut down api server
		shutdownServer()
	}

	return fakeKubeClient, kubeWatch, clustopClient, capiClient, fakeCAPIClient, shutdown
}
