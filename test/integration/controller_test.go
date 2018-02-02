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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/glog"
	"github.com/stretchr/testify/assert"

	kbatch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	// avoid error `clusteroperator/v1alpha1 is not enabled`
	_ "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/install"

	servertesting "github.com/openshift/cluster-operator/cmd/cluster-operator-apiserver/app/testing"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	coinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	acceptcontroller "github.com/openshift/cluster-operator/pkg/controller/accept"
	clustercontroller "github.com/openshift/cluster-operator/pkg/controller/cluster"
	infracontroller "github.com/openshift/cluster-operator/pkg/controller/infra"
	machinecontroller "github.com/openshift/cluster-operator/pkg/controller/machine"
	machinesetcontroller "github.com/openshift/cluster-operator/pkg/controller/machineset"
	mastercontroller "github.com/openshift/cluster-operator/pkg/controller/master"
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
		machineSets []v1alpha1.ClusterMachineSet
	}{
		{
			name: "no compute machine sets",
			machineSets: []v1alpha1.ClusterMachineSet{
				{
					Name: "master",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeMaster,
						Infra:    true,
						Size:     3,
					},
				},
			},
		},
		{
			name: "single compute machine sets",
			machineSets: []v1alpha1.ClusterMachineSet{
				{
					Name: "master",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeMaster,
						Infra:    true,
						Size:     3,
					},
				},
				{
					Name: "first",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     5,
					},
				},
			},
		},
		{
			name: "multiple compute machine sets",
			machineSets: []v1alpha1.ClusterMachineSet{
				{
					Name: "master",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeMaster,
						Infra:    true,
						Size:     3,
					},
				},
				{
					Name: "first",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     5,
					},
				},
				{
					Name: "second",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     6,
					},
				},
				{
					Name: "3rd",
					MachineSetConfig: v1alpha1.MachineSetConfig{
						NodeType: v1alpha1.NodeTypeCompute,
						Infra:    false,
						Size:     3,
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kubeClient, _, clusterOperatorClient, tearDown := startServerAndControllers(t)
			defer tearDown()

			clusterVersion := &v1alpha1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterVersionName,
				},
				Spec: v1alpha1.ClusterVersionSpec{
					ImageFormat: "openshift/origin-${component}:${version}",
					YumRepositories: []v1alpha1.YumRepository{
						{
							ID:       "testrepo",
							Name:     "a testing repo",
							BaseURL:  "http://example.com/nobodycares/",
							Enabled:  1,
							GPGCheck: 1,
							GPGKey:   "http://example.com/notreal.gpg",
						},
					},
					VMImages: v1alpha1.VMImages{
						AWSImages: &v1alpha1.AWSVMImages{
							AMIByRegion: map[string]string{
								"us-east-1": "fakeami",
							},
						},
					},
					DeploymentType: v1alpha1.ClusterDeploymentTypeOrigin,
					Version:        "v3.7.0",
				},
			}
			clusterOperatorClient.ClusteroperatorV1alpha1().ClusterVersions(testNamespace).Create(clusterVersion)

			cluster := &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterName,
				},
				Spec: v1alpha1.ClusterSpec{
					ClusterVersionRef: v1alpha1.ClusterVersionReference{
						Namespace: clusterVersion.Namespace,
						Name:      clusterVersion.Name,
					},
					Hardware: v1alpha1.ClusterHardwareSpec{
						AWS: &v1alpha1.AWSClusterSpec{},
					},
					DefaultHardwareSpec: &v1alpha1.MachineSetHardwareSpec{
						AWS: &v1alpha1.MachineSetAWSHardwareSpec{
							InstanceType: "instance-type",
							AMIName:      "ami-name",
						},
					},
					MachineSets: tc.machineSets,
				},
			}

			clusterOperatorClient.ClusteroperatorV1alpha1().Clusters(testNamespace).Create(cluster)

			if err := waitForClusterToExist(clusterOperatorClient, testNamespace, testClusterName); err != nil {
				t.Fatalf("error waiting for Cluster to exist: %v", err)
			}

			if !verifyMachineSetsCreated(t, kubeClient, clusterOperatorClient, cluster) {
				return
			}

			if !verifyInfraProvision(t, kubeClient, clusterOperatorClient, cluster) {
				return
			}

			masterMachineSet, err := getMasterMachineSet(clusterOperatorClient, cluster)
			if !assert.NoError(t, err, "error getting master machine set") {
				return
			}
			if !assert.NotNil(t, masterMachineSet, "master machine set does not exist") {
				return
			}

			if !verifyMachineSetProvision(t, kubeClient, clusterOperatorClient, masterMachineSet) {
				return
			}

			if !verifyMachineSetInstall(t, kubeClient, clusterOperatorClient, masterMachineSet) {
				return
			}

			computeMachineSets, err := getComputeMachineSets(clusterOperatorClient, cluster)
			if !assert.NoError(t, err, "error getting compute machine sets") {
				return
			}

			for _, machineSet := range computeMachineSets {
				if !verifyMachineSetProvision(t, kubeClient, clusterOperatorClient, machineSet) {
					return
				}

				if !verifyMachineSetAccept(t, kubeClient, clusterOperatorClient, machineSet) {
					return
				}
			}
		})
	}
}

// startServerAndControllers creates new test controllers injected with
// fake clients and returns:
//
// - a fake kubernetes core api client
// - a cluster-operator api client
// - a function for shutting down the controller and API server
//
// If there is an error, startServerAndControllers calls 'Fatal' on the
// injected testing.T.
func startServerAndControllers(t *testing.T) (
	*kubefake.Clientset,
	watch.Interface,
	clientset.Interface,
	func()) {

	// create a fake kube client
	fakePtr := clientgotesting.Fake{}
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	kubefake.AddToScheme(scheme)
	objectTracker := clientgotesting.NewObjectTracker(scheme, codecs.UniversalDecoder())
	kubeWatch := watch.NewFake()
	// Add a reactor for sending watch events when a job is modified
	objectReaction := clientgotesting.ObjectReaction(objectTracker)
	fakePtr.AddReactor("*", "jobs", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		var deletedObj runtime.Object
		if action, ok := action.(*clientgotesting.DeleteActionImpl); ok {
			deletedObj, _ = objectTracker.Get(action.GetResource(), action.GetNamespace(), action.GetName())
		}
		handled, obj, err := objectReaction(action)
		switch action.(type) {
		case clientgotesting.CreateActionImpl:
			kubeWatch.Add(obj)
		case clientgotesting.UpdateActionImpl:
			kubeWatch.Modify(obj)
		case clientgotesting.DeleteActionImpl:
			if deletedObj == nil {
				kubeWatch.Delete(deletedObj)
			}
		}
		return handled, obj, err
	})
	fakePtr.AddWatchReactor("*", clientgotesting.DefaultWatchReactor(kubeWatch, nil))
	// Create actual fake kube client
	fakeKubeClient := &kubefake.Clientset{Fake: fakePtr}

	// create a cluster-operator client and running server
	clusterOperatorClientConfig, shutdownServer := servertesting.StartTestServerOrDie(t)
	clusterOperatorClient, err := clientset.NewForConfig(clusterOperatorClientConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// create informers
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 10*time.Second)
	batchSharedInformers := kubeInformerFactory.Batch().V1()
	coInformerFactory := coinformers.NewSharedInformerFactory(clusterOperatorClient, 10*time.Second)
	coSharedInformers := coInformerFactory.Clusteroperator().V1alpha1()

	// create controllers
	ansibleImage := "test-ansible-image"
	ansibleImagePullPolicy := kapi.PullNever
	stopCh := make(chan struct{})
	t.Log("controller start")
	// Note that controllers must be created prior to starting the informers.
	// Otherwise, the controllers will not get the initial sync from the
	// informer and will time out waiting to sync.
	runControllers := []func(){
		func() func() {
			controller := clustercontroller.NewController(
				coSharedInformers.Clusters(),
				coSharedInformers.MachineSets(),
				coSharedInformers.ClusterVersions(),
				fakeKubeClient,
				clusterOperatorClient,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		func() func() {
			controller := infracontroller.NewController(
				coSharedInformers.Clusters(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clusterOperatorClient,
				ansibleImage,
				ansibleImagePullPolicy,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		func() func() {
			controller := machinesetcontroller.NewController(
				coSharedInformers.Clusters(),
				coSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clusterOperatorClient,
				ansibleImage,
				ansibleImagePullPolicy,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		func() func() {
			controller := mastercontroller.NewController(
				coSharedInformers.Clusters(),
				coSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clusterOperatorClient,
				ansibleImage,
				ansibleImagePullPolicy,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		func() func() {
			controller := acceptcontroller.NewController(
				coSharedInformers.Clusters(),
				coSharedInformers.MachineSets(),
				batchSharedInformers.Jobs(),
				fakeKubeClient,
				clusterOperatorClient,
				ansibleImage,
				ansibleImagePullPolicy,
			)
			return func() { controller.Run(1, stopCh) }
		}(),
		func() func() {
			controller := machinecontroller.NewController(
				coSharedInformers.Machines(),
				fakeKubeClient,
				clusterOperatorClient,
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
	coInformerFactory.Start(stopCh)

	shutdown := func() {
		// Shut down controller
		close(stopCh)
		// Wait for all controller to stop
		wg.Wait()
		// Shut down api server
		shutdownServer()
	}

	return fakeKubeClient, kubeWatch, clusterOperatorClient, shutdown
}

func getCluster(client clientset.Interface, namespace, name string) (*v1alpha1.Cluster, error) {
	return client.ClusteroperatorV1alpha1().Clusters(namespace).Get(name, metav1.GetOptions{})
}

func getMachineSet(client clientset.Interface, namespace, name string) (*v1alpha1.MachineSet, error) {
	return client.ClusteroperatorV1alpha1().MachineSets(namespace).Get(name, metav1.GetOptions{})
}

func getMasterMachineSet(client clientset.Interface, cluster *v1alpha1.Cluster) (*v1alpha1.MachineSet, error) {
	storedCluster, err := getCluster(client, cluster.Namespace, cluster.Name)
	if err != nil {
		return nil, err
	}
	return client.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).Get(storedCluster.Status.MasterMachineSetName, metav1.GetOptions{})
}

func getComputeMachineSets(client clientset.Interface, cluster *v1alpha1.Cluster) ([]*v1alpha1.MachineSet, error) {
	storedCluster, err := getCluster(client, cluster.Namespace, cluster.Name)
	if err != nil {
		return nil, err
	}
	machineSetList, err := client.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	machineSets := []*v1alpha1.MachineSet{}
	for _, machineSet := range machineSetList.Items {
		if machineSet.Spec.NodeType == v1alpha1.NodeTypeMaster {
			continue
		}
		if metav1.IsControlledBy(&machineSet, storedCluster) {
			machineSets = append(machineSets, machineSet.DeepCopy())
		}
	}
	return machineSets, nil
}

func getJob(kubeClient *kubefake.Clientset, namespace, name string) (*kbatch.Job, error) {
	return kubeClient.Batch().Jobs(namespace).Get(name, metav1.GetOptions{})
}

// waitForClusterToExist waits for the Cluster with the specified name to
// exist in the specified namespace.
func waitForClusterToExist(client clientset.Interface, namespace string, name string) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			glog.V(5).Infof("Waiting for cluster %s/%s to exist", namespace, name)
			_, err := getCluster(client, namespace, name)
			if nil == err {
				return true, nil
			}

			return false, nil
		},
	)
}

// waitForClusterMachineSetCount waits for the status of the cluster to
// reflect that the machine sets have been created.
func waitForClusterMachineSetCount(client clientset.Interface, namespace string, name string) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			glog.V(5).Infof("Waiting for machine sets for cluster %s/%s to be created", namespace, name)
			cluster, err := getCluster(client, namespace, name)
			if err != nil {
				return false, nil
			}
			if cluster.Status.MachineSetCount == len(cluster.Spec.MachineSets) {
				return true, nil
			}
			return false, nil
		},
	)
}

// waitForClusterStatus waits for the cluster to have a specified status.
func waitForClusterStatus(client clientset.Interface, namespace, name string, statusCheck func(*v1alpha1.Cluster) bool) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			cluster, err := getCluster(client, namespace, name)
			if err != nil {
				return false, nil
			}
			return statusCheck(cluster), nil
		},
	)
}

// waitForClusterProvisioning waits for the cluster to be in a state of provisioning.
func waitForClusterProvisioning(client clientset.Interface, namespace, name string) error {
	return waitForClusterStatus(client, namespace, name, func(cluster *v1alpha1.Cluster) bool {
		return cluster.Status.ProvisionJob != nil
	})
}

// waitForClusterProvisioned waits for the cluster to be provisioned.
func waitForClusterProvisioned(client clientset.Interface, namespace, name string) error {
	return waitForClusterStatus(client, namespace, name, func(cluster *v1alpha1.Cluster) bool {
		return cluster.Status.Provisioned
	})
}

// waitForMachineSetStatus waits for the machine set to have a specified status.
func waitForMachineSetStatus(client clientset.Interface, namespace, name string, statusCheck func(*v1alpha1.MachineSet) bool) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			machineSet, err := getMachineSet(client, namespace, name)
			if err != nil {
				return false, nil
			}
			return statusCheck(machineSet), nil
		},
	)
}

func verifyMachineSetsCreated(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, cluster *v1alpha1.Cluster) bool {
	if err := waitForClusterMachineSetCount(clusterOperatorClient, cluster.Namespace, cluster.Name); err != nil {
		t.Fatalf("error waiting for machine sets to be created for cluster: %v", err)
	}

	machineSets, err := clusterOperatorClient.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error getting the machine sets: %v", err)
	}

	if e, a := len(cluster.Spec.MachineSets), len(machineSets.Items); e != a {
		t.Fatalf("unexpected number of machine sets: expected %v, got %v", e, a)
	}

	type details struct {
		namePrefix string
		nodeType   v1alpha1.NodeType
		size       int
	}

	expectedDetails := make([]details, len(cluster.Spec.MachineSets))
	for i, ms := range cluster.Spec.MachineSets {
		expectedDetails[i] = details{
			namePrefix: fmt.Sprintf("%s-%s", cluster.Name, ms.Name),
			nodeType:   ms.NodeType,
			size:       ms.Size,
		}
	}

	actualDetails := make([]details, len(machineSets.Items))
	for i, ng := range machineSets.Items {
		lastDashInName := strings.LastIndexByte(ng.Name, '-')
		if lastDashInName < 0 {
			t.Fatalf("MachineSet name does not contain any dashes")
		}
		actualDetails[i] = details{
			namePrefix: ng.Name[:lastDashInName],
			nodeType:   ng.Spec.NodeType,
			size:       ng.Spec.Size,
		}
	}

	sortDetails := func(detailsSlice []details) {
		sort.Slice(detailsSlice, func(i, j int) bool {
			return detailsSlice[i].namePrefix < detailsSlice[j].namePrefix
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

func verifyInfraProvision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, cluster *v1alpha1.Cluster) bool {
	if err := waitForClusterProvisioning(clusterOperatorClient, cluster.Namespace, cluster.Name); err != nil {
		t.Fatalf("error waiting for cluster to be provisioning: %v", err)
	}

	storedCluster, err := getCluster(clusterOperatorClient, cluster.Namespace, cluster.Name)
	if !assert.NoError(t, err, "error getting cluster") {
		return false
	}
	if !assert.NotNil(t, storedCluster, "expecting cluster to exist") {
		return false
	}
	if !assert.NotNil(t, storedCluster.Status.ProvisionJob, "expecting cluster to be associated with an infra job") {
		return false
	}
	infraJob, err := getJob(kubeClient, cluster.Namespace, storedCluster.Status.ProvisionJob.Name)
	if !assert.NoError(t, err, "error getting infra job") {
		return false
	}
	if !assert.NotNil(t, infraJob, "expecting infra job to exist") {
		return false
	}

	if err := completeJob(kubeClient, infraJob); err != nil {
		t.Fatalf("error updating infra job: %v", err)
	}

	if err := waitForClusterProvisioned(clusterOperatorClient, cluster.Namespace, cluster.Name); err != nil {
		t.Fatalf("error waiting for cluster to be provisioned: %v", err)
	}

	return true
}

func verifyMachineSetJobCompletion(
	t *testing.T,
	kubeClient *kubefake.Clientset,
	clusterOperatorClient clientset.Interface,
	machineSet *v1alpha1.MachineSet,
	getJobRef func(*v1alpha1.MachineSet) *corev1.LocalObjectReference,
	isJobCompleted func(*v1alpha1.MachineSet) bool,
) bool {
	if err := waitForMachineSetStatus(
		clusterOperatorClient,
		machineSet.Namespace,
		machineSet.Name,
		func(machineSet *v1alpha1.MachineSet) bool { return getJobRef(machineSet) != nil },
	); err != nil {
		t.Fatalf("error waiting for job creation for machine set: %v", err)
	}

	storedMachineSet, err := getMachineSet(clusterOperatorClient, machineSet.Namespace, machineSet.Name)
	if !assert.NoError(t, err, "error getting machine set") {
		return false
	}
	if !assert.NotNil(t, storedMachineSet, "expecting machine set to exist") {
		return false
	}
	jobRef := getJobRef(storedMachineSet)
	if !assert.NotNil(t, jobRef, "expecting machine set to be associated with a job") {
		return false
	}
	job, err := getJob(kubeClient, machineSet.Namespace, jobRef.Name)
	if !assert.NoError(t, err, "error getting job") {
		return false
	}
	if !assert.NotNil(t, job, "expecting job to exist") {
		return false
	}

	if err := completeJob(kubeClient, job); err != nil {
		t.Fatalf("error updating job: %v", err)
	}

	if err := waitForMachineSetStatus(clusterOperatorClient, machineSet.Namespace, machineSet.Name, isJobCompleted); err != nil {
		t.Fatalf("error waiting for job completion for machine set: %v", err)
	}

	return true
}

func verifyMachineSetProvision(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	return verifyMachineSetJobCompletion(
		t,
		kubeClient,
		clusterOperatorClient,
		machineSet,
		func(machineSet *v1alpha1.MachineSet) *corev1.LocalObjectReference {
			return machineSet.Status.ProvisionJob
		},
		func(machineSet *v1alpha1.MachineSet) bool { return machineSet.Status.Provisioned },
	)
}

func verifyMachineSetInstall(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	return verifyMachineSetJobCompletion(
		t,
		kubeClient,
		clusterOperatorClient,
		machineSet,
		func(machineSet *v1alpha1.MachineSet) *corev1.LocalObjectReference {
			return machineSet.Status.InstallationJob
		},
		func(machineSet *v1alpha1.MachineSet) bool { return machineSet.Status.Installed },
	)
}

func verifyMachineSetAccept(t *testing.T, kubeClient *kubefake.Clientset, clusterOperatorClient clientset.Interface, machineSet *v1alpha1.MachineSet) bool {
	return verifyMachineSetJobCompletion(
		t,
		kubeClient,
		clusterOperatorClient,
		machineSet,
		func(machineSet *v1alpha1.MachineSet) *corev1.LocalObjectReference {
			return machineSet.Status.AcceptJob
		},
		func(machineSet *v1alpha1.MachineSet) bool { return machineSet.Status.Accepted },
	)
}
func completeJob(kubeClient *kubefake.Clientset, job *kbatch.Job) error {
	job.Status.Conditions = []kbatch.JobCondition{
		{
			Type:   kbatch.JobComplete,
			Status: kapi.ConditionTrue,
		},
	}
	_, err := kubeClient.Batch().Jobs(job.Namespace).Update(job)
	return err
}
