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
	"sort"
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
	clustercontroller "github.com/openshift/cluster-operator/pkg/controller/cluster"
	capimachinecontroller "github.com/openshift/cluster-operator/pkg/controller/clusterapimachine"
	componentscontroller "github.com/openshift/cluster-operator/pkg/controller/components"
	deployclusterapicontroller "github.com/openshift/cluster-operator/pkg/controller/deployclusterapi"
	infracontroller "github.com/openshift/cluster-operator/pkg/controller/infra"
	machinecontroller "github.com/openshift/cluster-operator/pkg/controller/machine"
	machinesetcontroller "github.com/openshift/cluster-operator/pkg/controller/machineset"
	mastercontroller "github.com/openshift/cluster-operator/pkg/controller/master"
	nodeconfigcontroller "github.com/openshift/cluster-operator/pkg/controller/nodeconfig"
	syncmachinesetcontroller "github.com/openshift/cluster-operator/pkg/controller/syncmachineset"
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

// TestClusterCreate tests that creating a cluster creates the machine sets
// for the cluster.
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
						NodeType: clustopv1alpha1.NodeTypeMaster,
						Infra:    true,
						Size:     3,
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
						NodeType: clustopv1alpha1.NodeTypeMaster,
						Infra:    true,
						Size:     3,
					},
				},
				{
					ShortName: "first",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     5,
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
						NodeType: clustopv1alpha1.NodeTypeMaster,
						Infra:    true,
						Size:     3,
					},
				},
				{
					ShortName: "first",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     5,
					},
				},
				{
					ShortName: "second",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     6,
					},
				},
				{
					ShortName: "3rd",
					MachineSetConfig: clustopv1alpha1.MachineSetConfig{
						NodeType: clustopv1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     3,
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kubeClient, _, clustopClient, _, _, tearDown := startServerAndControllers(t)
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

			cluster := &clustopv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterName,
				},
				Spec: clustopv1alpha1.ClusterSpec{
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
				},
			}

			cluster, err := clustopClient.ClusteroperatorV1alpha1().Clusters(testNamespace).Create(cluster)
			if !assert.NoError(t, err, "could not create the cluster") {
				return
			}

			if err := waitForClusterToExist(clustopClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to exist: %v", err)
			}

			if !verifyMachineSetsCreated(t, kubeClient, clustopClient, cluster) {
				return
			}

			if !completeInfraProvision(t, kubeClient, clustopClient, cluster) {
				return
			}

			masterMachineSet, err := getMasterMachineSet(clustopClient, cluster)
			if !assert.NoError(t, err, "error getting master machine set") {
				return
			}
			if !assert.NotNil(t, masterMachineSet, "master machine set does not exist") {
				return
			}

			if !completeMachineSetProvision(t, kubeClient, clustopClient, masterMachineSet) {
				return
			}

			if !completeControlPlaneInstall(t, kubeClient, clustopClient, cluster) {
				return
			}

			if !completeComponentsInstall(t, kubeClient, clustopClient, cluster) {
				return
			}

			if !completeNodeConfigInstall(t, kubeClient, clustopClient, cluster) {
				return
			}

			if !completeDeployClusterAPIInstall(t, kubeClient, clustopClient, cluster) {
				return
			}

			computeMachineSets, err := getComputeMachineSets(clustopClient, cluster)
			if !assert.NoError(t, err, "error getting compute machine sets") {
				return
			}

			for _, machineSet := range computeMachineSets {
				if !completeMachineSetProvision(t, kubeClient, clustopClient, machineSet) {
					return
				}
			}

			if err := waitForClusterReady(clustopClient, cluster.Namespace, cluster.Name); err != nil {
				t.Fatalf("timed out waiting for cluster to be ready: %v", err)
			}

			if err := clustopClient.ClusteroperatorV1alpha1().Clusters(cluster.Namespace).Delete(cluster.Name, &metav1.DeleteOptions{}); err != nil {
				t.Fatalf("could not delete cluster: %v", err)
			}

			if !completeMachineSetsDeprovision(t, kubeClient, clustopClient, cluster) {
				return
			}

			if !completeInfraDeprovision(t, kubeClient, clustopClient, cluster) {
				return
			}

			if err := waitForClusterToNotExist(clustopClient, cluster.Namespace, cluster.Name); err != nil {
				t.Fatalf("cluster not removed: %v", err)
			}
		})
	}
}

func testCAPIMachineSet(name string, replicas int32, roles []capicommon.MachineRole, infra bool) *capiv1alpha1.MachineSet {
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

// TestCAPIClusterCreate tests creating a cluster-api cluster.
func TestCAPIClusterCreate(t *testing.T) {
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

			coClusterSpec := &clustopv1alpha1.ClusterSpec{
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
						Value: clusterAPIProviderConfigFromClusterSpec(coClusterSpec),
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

				machineSet := testCAPIMachineSet(
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

			if err := waitForCAPIClusterToExist(capiClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to exist: %v", err)
			}

			for _, ms := range machineSets {
				if err := waitForCAPIMachineSetToExist(capiClient, testNamespace, ms.Name); err != nil {
					t.Fatalf("error waiting for MachineSet %v to exist: %v", ms.Name, err)
				}
			}

			if !completeCAPIInfraProvision(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeCAPIControlPlaneInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeCAPIComponentsInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeCAPINodeConfigInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if !completeCAPIDeployClusterAPIInstall(t, kubeClient, capiClient, cluster) {
				return
			}

			if err := capiClient.ClusterV1alpha1().Clusters(cluster.Namespace).Delete(cluster.Name, &metav1.DeleteOptions{}); err != nil {
				t.Fatalf("could not delete cluster: %v", err)
			}

			if err := setCAPIDeprovisionedComputeMachinesets(capiClient, cluster.Namespace, cluster.Name); err != nil {
				t.Fatalf("could not set DeprovisionedComputeMachinesets: %v", err)
			}

			if !completeCAPIInfraDeprovision(t, kubeClient, capiClient, cluster) {
				return
			}

			if err := waitForCAPIClusterToNotExist(capiClient, cluster.Namespace, cluster.Name); err != nil {
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
		// cluster
		func() func() {
			controller := clustercontroller.NewController(
				clustopSharedInformers.Clusters(),
				clustopSharedInformers.MachineSets(),
				clustopSharedInformers.ClusterVersions(),
				fakeKubeClient,
				clustopClient,
			)
			controller.BuildRemoteClient = func(*clustopv1alpha1.Cluster) (capiclientset.Interface, error) {
				return fakeCAPIClient, nil
			}
			return func() { controller.Run(1, stopCh) }
		}(),
		// infra
		func() func() {
			controller := infracontroller.NewClusterOperatorController(
				clustopSharedInformers.Clusters(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// machineset
		func() func() {
			controller := machinesetcontroller.NewController(
				clustopSharedInformers.MachineSets(),
				clustopSharedInformers.Clusters(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// master
		func() func() {
			controller := mastercontroller.NewClustopController(
				clustopSharedInformers.Clusters(),
				clustopSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// machine
		func() func() {
			controller := machinecontroller.NewController(
				clustopSharedInformers.Machines(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// components
		func() func() {
			controller := componentscontroller.NewClustopController(
				clustopSharedInformers.Clusters(),
				clustopSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// nodeconfig
		func() func() {
			controller := nodeconfigcontroller.NewClustopController(
				clustopSharedInformers.Clusters(),
				clustopSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// deployclusterapi
		func() func() {
			controller := deployclusterapicontroller.NewClustopController(
				clustopSharedInformers.Clusters(),
				clustopSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// syncmachineset
		func() func() {
			controller := syncmachinesetcontroller.NewController(
				clustopSharedInformers.MachineSets(),
				clustopSharedInformers.Clusters(),
				fakeKubeClient,
				clustopClient,
			)
			controller.BuildRemoteClient = func(*clustopv1alpha1.Cluster) (capiclientset.Interface, error) {
				return fakeCAPIClient, nil
			}
			return func() { controller.Run(1, stopCh) }
		}(),
		// cpai-infra
		func() func() {
			controller := infracontroller.NewClusterAPIController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// cpai-machine
		func() func() {
			controller := capimachinecontroller.NewController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.Machines(),
				clustopSharedInformers.ClusterVersions(),
				fakeKubeClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// capi-master
		func() func() {
			controller := mastercontroller.NewCAPIController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// capi-components
		func() func() {
			controller := componentscontroller.NewCAPIController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// capi-nodeconfig
		func() func() {
			controller := nodeconfigcontroller.NewCAPIController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clustopClient,
				capiClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		// capi-deployclusterapi
		func() func() {
			controller := deployclusterapicontroller.NewCAPIController(
				capiSharedInformers.Clusters(),
				capiSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
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

func getCluster(clustopClient clustopclientset.Interface, namespace, name string) (*clustopv1alpha1.Cluster, error) {
	return clustopClient.ClusteroperatorV1alpha1().Clusters(namespace).Get(name, metav1.GetOptions{})
}

func getCAPICluster(capiClient capiclientset.Interface, namespace, name string) (*capiv1alpha1.Cluster, error) {
	return capiClient.ClusterV1alpha1().Clusters(namespace).Get(name, metav1.GetOptions{})
}

func getMachineSet(clustopClient clustopclientset.Interface, namespace, name string) (*clustopv1alpha1.MachineSet, error) {
	return clustopClient.ClusteroperatorV1alpha1().MachineSets(namespace).Get(name, metav1.GetOptions{})
}

func getCAPIMachineSet(capiClient capiclientset.Interface, namespace, name string) (*capiv1alpha1.MachineSet, error) {
	return capiClient.ClusterV1alpha1().MachineSets(namespace).Get(name, metav1.GetOptions{})
}

func getMasterMachineSet(clustopClient clustopclientset.Interface, cluster *clustopv1alpha1.Cluster) (*clustopv1alpha1.MachineSet, error) {
	storedCluster, err := getCluster(clustopClient, cluster.Namespace, cluster.Name)
	if err != nil {
		return nil, err
	}
	return clustopClient.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).Get(storedCluster.Status.MasterMachineSetName, metav1.GetOptions{})
}

func getMachineSetsForCluster(clustopClient clustopclientset.Interface, cluster *clustopv1alpha1.Cluster) ([]*clustopv1alpha1.MachineSet, error) {
	machineSetList, err := clustopClient.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	machineSets := []*clustopv1alpha1.MachineSet{}
	for _, machineSet := range machineSetList.Items {
		if metav1.IsControlledBy(&machineSet, cluster) {
			machineSets = append(machineSets, machineSet.DeepCopy())
		}
	}
	return machineSets, nil
}

func getComputeMachineSets(clustopClient clustopclientset.Interface, cluster *clustopv1alpha1.Cluster) ([]*clustopv1alpha1.MachineSet, error) {
	allMachineSets, err := getMachineSetsForCluster(clustopClient, cluster)
	if err != nil {
		return nil, err
	}
	computeMachineSets := []*clustopv1alpha1.MachineSet{}
	for _, machineSet := range allMachineSets {
		if machineSet.Spec.NodeType == clustopv1alpha1.NodeTypeCompute {
			computeMachineSets = append(computeMachineSets, machineSet)
		}
	}
	return computeMachineSets, nil
}

func getJob(kubeClient *kubefake.Clientset, namespace, name string) (*kbatch.Job, error) {
	return kubeClient.Batch().Jobs(namespace).Get(name, metav1.GetOptions{})
}

func verifyMachineSetsCreated(t *testing.T, kubeClient *kubefake.Clientset, clustopClient clustopclientset.Interface, cluster *clustopv1alpha1.Cluster) bool {
	if err := waitForClusterMachineSetCount(clustopClient, cluster.Namespace, cluster.Name); err != nil {
		t.Fatalf("error waiting for machine sets to be created for cluster: %v", err)
	}

	machineSets, err := clustopClient.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error getting the machine sets: %v", err)
	}

	if e, a := len(cluster.Spec.MachineSets), len(machineSets.Items); e != a {
		t.Fatalf("unexpected number of machine sets: expected %v, got %v", e, a)
	}

	type details struct {
		clusterName string
		shortName   string
		nodeType    clustopv1alpha1.NodeType
		size        int
	}

	expectedDetails := make([]details, len(cluster.Spec.MachineSets))
	for i, ms := range cluster.Spec.MachineSets {
		expectedDetails[i] = details{
			clusterName: cluster.Name,
			shortName:   ms.ShortName,
			nodeType:    ms.NodeType,
			size:        ms.Size,
		}
	}

	actualDetails := make([]details, len(machineSets.Items))
	for i, ms := range machineSets.Items {
		if ms.Labels == nil {
			t.Fatalf("machine set does not have any labels")
		}
		actualDetails[i] = details{
			clusterName: ms.Labels["cluster"],
			shortName:   ms.Labels["machine-set-short-name"],
			nodeType:    ms.Spec.NodeType,
			size:        ms.Spec.Size,
		}
	}

	sortDetails := func(detailsSlice []details) {
		sort.Slice(detailsSlice, func(i, j int) bool {
			switch {
			case detailsSlice[i].clusterName < detailsSlice[j].clusterName:
				return true
			case detailsSlice[i].clusterName > detailsSlice[j].clusterName:
				return false
			default:
				return detailsSlice[i].shortName < detailsSlice[j].shortName
			}
		})
	}
	sortDetails(expectedDetails)
	sortDetails(actualDetails)

	if e, a := len(expectedDetails), len(actualDetails); e != a {
		t.Fatalf("unexpected number of machine sets: expected %q, got %q", e, a)
	}

	for i, a := range actualDetails {
		e := expectedDetails[i]
		if e != a {
			t.Fatalf("unexpected machine set: expected %+v, got %+v", e, a)
		}
	}

	return true
}

func deleteDependentMachineSets(clustopClient clustopclientset.Interface, cluster *clustopv1alpha1.Cluster) error {
	machineSets, err := getMachineSetsForCluster(clustopClient, cluster)
	if err != nil {
		return err
	}
	for _, machineSet := range machineSets {
		if err := clustopClient.ClusteroperatorV1alpha1().MachineSets(machineSet.Namespace).Delete(machineSet.Name, &metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func clusterAPIProviderConfigFromClusterSpec(clusterSpec *clustopv1alpha1.ClusterSpec) *runtime.RawExtension {
	clusterProviderConfigSpec := &clustopv1alpha1.ClusterProviderConfigSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clustopv1alpha1.SchemeGroupVersion.String(),
			Kind:       "ClusterProviderConfigSpec",
		},
		ClusterSpec: *clusterSpec,
	}
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, api.Scheme, api.Scheme, false)
	var buffer bytes.Buffer
	serializer.Encode(clusterProviderConfigSpec, &buffer)
	return &runtime.RawExtension{
		Raw: buffer.Bytes(),
	}
}

func setCAPIDeprovisionedComputeMachinesets(capiClient capiclientset.Interface, namespace, name string) error {
	cluster, err := getCAPICluster(capiClient, namespace, name)
	if err != nil {
		return fmt.Errorf("could not get cluster %s/%s", namespace, name)
	}
	status, err := controller.ClusterStatusFromClusterAPI(cluster)
	if err != nil {
		return err
	}
	status.DeprovisionedComputeMachinesets = true
	providerStatus, err := controller.ClusterAPIProviderStatusFromClusterStatus(status)
	if err != nil {
		return err
	}
	cluster.Status.ProviderStatus = providerStatus
	_, err = capiClient.ClusterV1alpha1().Clusters(namespace).UpdateStatus(cluster)
	if err != nil {
		return err
	}
	return nil
}
