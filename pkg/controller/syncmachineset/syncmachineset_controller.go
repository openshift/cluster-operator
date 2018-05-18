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

package syncmachineset

import (
	"bytes"
	"fmt"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	coapi "github.com/openshift/cluster-operator/pkg/api"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	"github.com/openshift/cluster-operator/pkg/logging"

	clusterapiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a machineset.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	controllerLogName = "syncmachineset"

	remoteClusterAPINamespace      = "kube-cluster"
	remoteClusterAPIDeploymentName = "cluster-api-apiserver"
	errorMsgClusterAPINotInstalled = "cannot sync until cluster API is installed and ready"
)

// NewController returns a new *Controller.
func NewController(
	machineSetInformer informers.MachineSetInformer,
	clusterInformer informers.ClusterInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient coclient.Interface,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_syncmachineset_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)

	c := &Controller{
		client:     clusteroperatorClient,
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "syncmachineset"),
		logger:     logger,
	}

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		// TODO: delete
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	c.clusterLister = clusterInformer.Lister()

	c.syncHandler = c.syncMachineSet
	c.enqueueMachineSet = c.enqueue
	c.buildClients = c.buildRemoteClusterClients

	return c
}

// Controller manages syncing cluster and machineset objects with a
// remote cluster API
type Controller struct {
	client     coclient.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncMachineSet for testing.
	syncHandler func(key string) error

	// Used for unit testing
	enqueueMachineSet func(machineSet *cov1.MachineSet)

	buildClients func(*cov1.MachineSet) (clusterapiclient.Interface, error)

	// machineSetsLister is able to list/get machine sets and is
	// populated by the shared informer passed to NewController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared
	// informer has been synced at least once.  Added as a member
	// to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// clusterLister is able to list/get clusters and is populated
	// by the shared informer passed to NewController.
	clusterLister lister.ClusterLister
	// clusterSynced returns true if the cluster shared informer
	// has been synced at least once. Added as a member to the
	// struct to allow injection for testing.
	clusterSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(*cov1.MachineSet)
	msLog := logging.WithMachineSet(c.logger, machineSet)
	if !isMasterMachineSet(machineSet) {
		msLog.Debugf("Adding machine set")
		c.enqueueMachineSet(machineSet)
		return
	}

	coCluster, err := controller.ClusterForMachineSet(machineSet, c.clusterLister)
	if err != nil {
		msLog.Debugf("error looking up machineset's cluster: %v", err)
		return
	}

	machineSets, err := controller.MachineSetsForCluster(coCluster, c.machineSetsLister)
	if err != nil {
		logging.WithCluster(c.logger, coCluster).Debugf("error looking up cluster's machinesets: %v", err)
		return
	}
	for _, ms := range machineSets {
		logging.WithMachineSet(c.logger, ms).Debugf("Adding machine set")
		c.enqueueMachineSet(ms)
	}
}

func (c *Controller) updateMachineSet(old, cur interface{}) {
	machineSet := cur.(*cov1.MachineSet)
	msLog := logging.WithMachineSet(c.logger, machineSet)
	if !isMasterMachineSet(machineSet) {
		msLog.Debugf("Updating machine set")
		c.enqueueMachineSet(machineSet)
		return
	}

	coCluster, err := controller.ClusterForMachineSet(machineSet, c.clusterLister)
	if err != nil {
		msLog.Debugf("error looking up machineset's cluster: %v", err)
		return
	}

	machineSets, err := controller.MachineSetsForCluster(coCluster, c.machineSetsLister)
	if err != nil {
		logging.WithCluster(c.logger, coCluster).Debugf("error looking up cluster's machinesets: %v", err)
		return
	}
	for _, ms := range machineSets {
		logging.WithMachineSet(c.logger, ms).Debugf("Updating machine set")
		c.enqueueMachineSet(ms)
	}
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machine sets will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("Starting syncmachineset controller")
	defer c.logger.Infof("Shutting down syncmachineset controller")

	if !controller.WaitForCacheSync("syncmachineset", stopCh, c.machineSetsSynced) {
		c.logger.Errorf("could not sync caches for syncmachineset controller")
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(machineSet *cov1.MachineSet) {
	key, err := controller.KeyFunc(machineSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", machineSet, err))
		return
	}

	c.queue.Add(key)
}

// enqueueAfter will enqueue a machine set after the provided amount of time.
func (c *Controller) enqueueAfter(ms *cov1.MachineSet, after time.Duration) {
	key, err := controller.KeyFunc(ms)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", ms, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key.(string))

	return true
}

func (c *Controller) handleErr(err error, key string) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	logger := c.logger.WithField("machineset", key)

	if c.queue.NumRequeues(key) < maxRetries {
		logger.Infof("error syncing machine set: %v", err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("dropping machine set out of the queue: %v", err)
	c.queue.Forget(key)
}

func isMasterMachineSet(machineSet *cov1.MachineSet) bool {
	return machineSet.Spec.NodeType == cov1.NodeTypeMaster
}

func (c *Controller) buildRemoteClusterClients(machineSet *cov1.MachineSet) (clusterapiclient.Interface, error) {
	// Load the kubeconfig secret for communicating with the remote cluster:

	// Step 1: Get a cluster from the machineset
	cluster, err := controller.ClusterForMachineSet(machineSet, c.clusterLister)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve cluster for machineset")
	}

	// Step 2: Get the secret ref from the cluster
	//
	// TODO: The cluster controller should watch secrets until it
	// sees a kubeconfig secret named after the cluster and then
	// update cluster status with a reference to that
	// secret. Until then we'll look for a
	// "clustername-kubeconfig" secret.
	//
	// clusterConfig := cluster.Status.AdminKubeconfig
	// if clusterConfig == nil {
	// 	return nil, fmt.Errorf("unable to retrieve AdminKubeConfig from cluster status")
	// }
	secretName := cluster.Name + "-kubeconfig"

	// Step 3: Retrieve secret
	secret, err := c.kubeClient.CoreV1().Secrets(machineSet.Namespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	secretData := secret.Data["kubeconfig"]

	// Step 4: Generate config from secret data
	config, err := clientcmd.Load(secretData)
	if err != nil {
		return nil, err
	}

	kubeConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{})
	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	// Step 5: Return client for remote cluster
	return clusterapiclient.NewForConfig(restConfig)
}

// syncMachineSet will sync the machine set with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *Controller) syncMachineSet(key string) error {
	startTime := time.Now()
	c.logger.WithField("key", key).Debug("syncing machineset")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing machineset")
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	machineSet, err := c.machineSetsLister.MachineSets(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Debug("machineset has been deleted")
		// TODO: ensure deleted in remote cluster. If so, how
		// are we going to figure out what cluster the machine
		// set was from to load the correct kubeconfig secret?
		return nil
	}
	if err != nil {
		return err
	}

	// Master machinesets are managed by the CO cluster and are
	// not synced with the remote cluster. When syncing a master
	// machineset, we will sync the cluster that the master
	// machineset belongs to rather than the master machineset
	// itself.
	if isMasterMachineSet(machineSet) {
		return c.syncMasterMachineSet(machineSet)
	}

	return c.syncComputeMachineSet(machineSet)
}

// Creates the cluster in the remote.
func (c *Controller) syncMasterMachineSet(ms *cov1.MachineSet) error {
	msLog := logging.WithMachineSet(c.logger, ms)
	msLog.Debug("loaded master machineset")

	// Lookup the CO cluster object for this machine set:
	coCluster, err := c.clusterLister.Clusters(ms.Namespace).Get(ms.Labels[controller.ClusterNameLabel])
	if err != nil {
		return fmt.Errorf("error looking up machineset's cluster: %v", err)
	}
	msLog = logging.WithCluster(msLog, coCluster)

	if !ms.Status.ClusterAPIInstalled {
		msLog.Debugf(errorMsgClusterAPINotInstalled)
		return nil
	}

	// Create the Cluster object if it does not already exist:
	remoteClusterAPIClient, err := c.buildClients(ms)
	if err != nil {
		return err
	}

	rCluster, err := remoteClusterAPIClient.ClusterV1alpha1().Clusters(remoteClusterAPINamespace).Get(coCluster.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		msLog.Infof("creating new cluster API object in remote cluster")
		rCluster, err = buildClusterAPICluster(coCluster)
		if err != nil {
			return fmt.Errorf("error building cluster api cluster object: %v", err)
		}
		_, err = remoteClusterAPIClient.ClusterV1alpha1().Clusters(remoteClusterAPINamespace).Create(rCluster)
		if err != nil {
			return fmt.Errorf("error creating cluster object in remote cluster: %v", err)
		}
	} else if err != nil {
		return err
	} else {
		// TODO: patch/update here?
		msLog.Debugf("cluster API object already exists in remote cluster")
	}

	return nil
}

// Creates machinesets in the remote.
func (c *Controller) syncComputeMachineSet(ms *cov1.MachineSet) error {
	msLog := logging.WithMachineSet(c.logger, ms)
	msLog.Debug("loaded compute machineset")

	// Lookup the CO cluster object for this machine set and use
	// that to get the master machine set for the cluster. Continue
	// syncing generic machine set if the master machine set is
	// installed (ms.Status.ClusterAPIInstalled == true).
	// TODO: Set a flag on cluster object once it has been synced
	// so that we don't have to retrieve the master machine set to
	// know if we can sync the other machine sets.
	coCluster, err := c.clusterLister.Clusters(ms.Namespace).Get(ms.Labels[controller.ClusterNameLabel])
	if err != nil {
		return fmt.Errorf("error looking up machineset's cluster: %v", err)
	}
	coMasterMachineSet, err := c.machineSetsLister.MachineSets(coCluster.Namespace).Get(coCluster.Status.MasterMachineSetName)
	if err != nil {
		return fmt.Errorf("error looking up cluster's master machineset: %v", err)
	}

	if !coMasterMachineSet.Status.ClusterAPIInstalled {
		msLog.Debugf(errorMsgClusterAPINotInstalled)
		return nil
	}

	remoteClusterAPIClient, err := c.buildClients(ms)
	if err != nil {
		return err
	}

	// Create machineSet object in remote if the machineSet does not already exist.
	remoteMachineSet, err := remoteClusterAPIClient.ClusterV1alpha1().MachineSets(remoteClusterAPINamespace).Get(ms.Name, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		msLog.Infof("creating new machineSet API object in remote cluster")

		// Lookup clusterVersion for machineSet
		coVersionNamespace, coVersionName := ms.Spec.ClusterVersionRef.Namespace, ms.Spec.ClusterVersionRef.Name
		coClusterVersion, err := c.client.ClusteroperatorV1alpha1().ClusterVersions(coVersionNamespace).Get(coVersionName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("cannot retrieve cluster version %s/%s: %v", coVersionNamespace, coVersionName, err)
		}

		remoteMachineSet, err = buildClusterAPIMachineSet(ms, coClusterVersion)
		if err != nil {
			return fmt.Errorf("error building cluster api machineset object: %v", err)
		}
		_, err = remoteClusterAPIClient.ClusterV1alpha1().MachineSets(remoteClusterAPINamespace).Create(remoteMachineSet)
		if err != nil {
			return fmt.Errorf("error creating machineSet API object in remote cluster: %v", err)
		}
	} else if err != nil {
		return err
	} else {
		// TODO: patch/update here?
		msLog.Debugf("machineSet API object already exists in remote cluster")
	}

	return nil
}

func buildClusterAPIMachineSet(machineSet *cov1.MachineSet, clusterVersion *cov1.ClusterVersion) (*clusterapiv1.MachineSet, error) {
	machineSet = machineSet.DeepCopy()

	capiMachineSet := clusterapiv1.MachineSet{}
	capiMachineSet.Name = machineSet.Name
	capiMachineSet.Namespace = remoteClusterAPINamespace
	replicas := int32(machineSet.Spec.Size)
	capiMachineSet.Spec.Replicas = &replicas
	capiMachineSet.Spec.Selector.MatchLabels = map[string]string{"machineset": machineSet.Name}

	if machineSet.Annotations == nil {
		machineSet.Annotations = map[string]string{}
	}
	sClusterVersion, err := serializeCOResource(clusterVersion)
	if err != nil {
		return nil, err
	}
	machineSet.Annotations["cluster-operator.openshift.io/cluster-version"] = string(sClusterVersion)

	machineTemplate := clusterapiv1.MachineTemplateSpec{}
	machineTemplate.Labels = map[string]string{"machineset": machineSet.Name}
	machineTemplate.Spec.Labels = map[string]string{"machineset": machineSet.Name}
	sMachineSet, err := serializeCOResource(machineSet)
	if err != nil {
		return nil, err
	}
	machineTemplate.Spec.ProviderConfig.Value = &runtime.RawExtension{
		Raw: sMachineSet,
	}
	capiMachineSet.Spec.Template = machineTemplate

	return &capiMachineSet, nil
}

func buildClusterAPICluster(cluster *cov1.Cluster) (*clusterapiv1.Cluster, error) {
	capiCluster := clusterapiv1.Cluster{}
	capiCluster.Name = cluster.Name
	sCluster, err := serializeCOResource(cluster)
	if err != nil {
		return nil, err
	}
	capiCluster.Spec.ProviderConfig.Value = &runtime.RawExtension{
		Raw: sCluster,
	}

	// These are unused, dummy values.
	capiCluster.Spec.ClusterNetwork.ServiceDomain = "cluster-api.k8s.io"
	capiCluster.Spec.ClusterNetwork.Pods.CIDRBlocks = []string{"10.10.0.0/16"}
	capiCluster.Spec.ClusterNetwork.Services.CIDRBlocks = []string{"172.30.0.0/16"}

	return &capiCluster, nil
}

func serializeCOResource(object runtime.Object) ([]byte, error) {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, coapi.Scheme, coapi.Scheme, false)
	encoder := coapi.Codecs.EncoderForVersion(serializer, cov1.SchemeGroupVersion)
	buffer := &bytes.Buffer{}
	err := encoder.Encode(object, buffer)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
