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
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/watch"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	// avoid error `clusteroperator/v1alpha1 is not enabled`
	_ "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/install"

	servertesting "github.com/openshift/cluster-operator/cmd/cluster-operator-apiserver/app/testing"
	"github.com/openshift/cluster-operator/pkg/api"
	clustopv1alpha1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	clustopinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/controller/awselb"
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
							r, _ := controller.ClusterAPIMachineProviderConfigFromMachineSetSpec(&clustopv1alpha1.MachineSetSpec{
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
					ShortName: "master",
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
					ShortName: "master",
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
					ShortName: "master",
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
					ImageFormat: "openshift/origin-${component}:${version}",
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
					DeploymentType:                  clustopv1alpha1.ClusterDeploymentTypeOrigin,
					Version:                         "v3.7.0",
					OpenshiftAnsibleImage:           func(s string) *string { return &s }("test-ansible-image"),
					OpenshiftAnsibleImagePullPolicy: func(p kapi.PullPolicy) *kapi.PullPolicy { return &p }(kapi.PullNever),
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
			cluster := &capiv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterName,
				},
				Spec: capiv1alpha1.ClusterSpec{
					ClusterNetwork: capiv1alpha1.ClusterNetworkingConfig{
						Services: capiv1alpha1.NetworkRanges{
							CIDRBlocks: []string{"255.255.255.255"},
						},
						Pods: capiv1alpha1.NetworkRanges{
							CIDRBlocks: []string{"255.255.255.255"},
						},
						ServiceDomain: "service-domain",
					},
					ProviderConfig: capiv1alpha1.ProviderConfig{
						Value: clusterAPIProviderConfigFromClusterSpec(clusterDeploymentSpec),
					},
				},
			}

			cluster, err := capiClient.ClusterV1alpha1().Clusters(testNamespace).Create(cluster)
			if !assert.NoError(t, err, "could not create the cluster") {
				return
			}

			machineSets := make([]*capiv1alpha1.MachineSet, len(tc.machineSets))
			var masterMachineSet *capiv1alpha1.MachineSet

			for i, clustopClusterMS := range tc.machineSets {

				role := capicommon.NodeRole
				if clustopClusterMS.NodeType == clustopv1alpha1.NodeTypeMaster {
					role = capicommon.MasterRole
				}

				machineSet := testMachineSet(
					fmt.Sprintf("%s-%s", cluster.Name, clustopClusterMS.ShortName),
					int32(clustopClusterMS.Size),
					[]capicommon.MachineRole{role},
					clustopClusterMS.Infra)

				if machineSet.Labels == nil {
					machineSet.Labels = map[string]string{}
				}
				machineSet.Labels["clusteroperator.openshift.io/cluster"] = cluster.Name
				ms, err := capiClient.ClusterV1alpha1().MachineSets(testNamespace).Create(machineSet)
				if !assert.NoError(t, err, "could not create machineset %v", machineSet.Name) {
					return
				}
				machineSets[i] = ms

				if clustopClusterMS.NodeType == clustopv1alpha1.NodeTypeMaster {
					if masterMachineSet != nil {
						t.Fatalf("multiple master machinesets: %s and %s", masterMachineSet.Name, ms.Name)
					}
					masterMachineSet = ms
				}
			}

			if masterMachineSet == nil {
				t.Fatalf("no master machineset")
			}

			if err := waitForClusterToExist(capiClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to exist: %v", err)
			}

			for _, ms := range machineSets {
				if err := waitForMachineSetToExist(capiClient, testNamespace, ms.Name); err != nil {
					t.Fatalf("error waiting for MachineSet %v to exist: %v", ms.Name, err)
				}
			}

			if !completeInfraProvision(t, kubeClient, capiClient, cluster) {
				return
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

			if err := capiClient.ClusterV1alpha1().Clusters(cluster.Namespace).Delete(cluster.Name, &metav1.DeleteOptions{}); err != nil {
				t.Fatalf("could not delete cluster: %v", err)
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
	capiInformerFactory := capiinformers.NewSharedInformerFactory(capiClient, 10*time.Second)
	capiSharedInformers := capiInformerFactory.Cluster().V1alpha1()

	// create controllers
	stopCh := make(chan struct{})
	t.Log("controller start")
	// Note that controllers must be created prior to starting the informers.
	// Otherwise, the controllers will not get the initial sync from the
	// informer and will time out waiting to sync.
	runControllers := []func(){
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

func getCluster(capiClient capiclientset.Interface, namespace, name string) (*capiv1alpha1.Cluster, error) {
	return capiClient.ClusterV1alpha1().Clusters(namespace).Get(name, metav1.GetOptions{})
}

func getMachineSet(capiClient capiclientset.Interface, namespace, name string) (*capiv1alpha1.MachineSet, error) {
	return capiClient.ClusterV1alpha1().MachineSets(namespace).Get(name, metav1.GetOptions{})
}

func getJob(kubeClient *kubefake.Clientset, namespace, name string) (*kbatch.Job, error) {
	return kubeClient.Batch().Jobs(namespace).Get(name, metav1.GetOptions{})
}

func clusterAPIProviderConfigFromClusterSpec(clusterSpec *clustopv1alpha1.ClusterDeploymentSpec) *runtime.RawExtension {
	clusterProviderConfigSpec := &clustopv1alpha1.ClusterProviderConfigSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clustopv1alpha1.SchemeGroupVersion.String(),
			Kind:       "ClusterProviderConfigSpec",
		},
		ClusterDeploymentSpec: *clusterSpec,
	}
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	serializer.Encode(clusterProviderConfigSpec, &buffer)
	return &runtime.RawExtension{
		Raw: buffer.Bytes(),
	}
}
