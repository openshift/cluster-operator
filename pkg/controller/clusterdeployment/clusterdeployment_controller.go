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

package clusterdeployment

import (
	"bytes"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	cometrics "github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	capilisters "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	coinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	colisters "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"

	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	cologging "github.com/openshift/cluster-operator/pkg/logging"
)

const (
	controllerLogName = "clusterdeployment"

	// versionMissingRegion indicates the cluster's desired version does not have an AMI defined for it's region.
	versionMissingRegion = "VersionMissingRegion"

	// versionHasRegion indicates that the cluster's desired version now has an AMI defined for it's region.
	versionHasRegion = "VersionHasRegion"

	// versionMissing indicates that the cluster's desired version does not yet exist
	versionMissing = "VersionMissing"

	// versionExists indicates that the cluster's desired version does exist
	versionExists = "VersionExists"

	// deleteAfterAnnotation is the annotation that contains a duration after which the cluster should be cleaned up.
	deleteAfterAnnotation = "clusteroperator.openshift.io/delete-after"
)

// NewController returns a new cluster deployment controller.
func NewController(
	clusterDeploymentInformer coinformers.ClusterDeploymentInformer,
	clusterInformer capiinformers.ClusterInformer,
	machineSetInformer capiinformers.MachineSetInformer,
	clusterVersionInformer coinformers.ClusterVersionInformer,
	kubeClient kubeclientset.Interface,
	clustopClient coclientset.Interface,
	capiClient capiclientset.Interface) *Controller {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		cometrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_clusterdeployment_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)

	c := &Controller{
		kubeClient:    kubeClient,
		clustopClient: clustopClient,
		capiClient:    capiClient,
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "clusterdeployment"),
		logger:        logger,
	}

	clusterDeploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addClusterDeployment,
		UpdateFunc: c.updateClusterDeployment,
		DeleteFunc: c.deleteClusterDeployment,
	})
	c.clusterDeploymentsLister = clusterDeploymentInformer.Lister()
	c.clusterDeploymentsSynced = clusterDeploymentInformer.Informer().HasSynced

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	machineSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMachineSet,
		UpdateFunc: c.updateMachineSet,
		DeleteFunc: c.deleteMachineSet,
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	c.clusterVersionsLister = clusterVersionInformer.Lister()

	c.syncHandler = c.syncClusterDeployment
	c.enqueueClusterDeployment = c.enqueue

	return c
}

// Controller manages clusters.
type Controller struct {
	clustopClient coclientset.Interface
	capiClient    capiclientset.Interface
	kubeClient    kubeclientset.Interface

	// To allow injection of syncClusterDeployment for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueClusterDeployment func(clusterDeployment *cov1.ClusterDeployment)

	// clusterDeploymentsLister is able to list/get cluster operator cluster deployments.
	clusterDeploymentsLister colisters.ClusterDeploymentLister

	// clusterDeploymentsSynced returns true if the cluster shared informer has been synced at least once.
	clusterDeploymentsSynced cache.InformerSynced

	// clustersLister is able to list/get cluster api clusters.
	// NewController.
	clustersLister capilisters.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister capilisters.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// clusterVersionsLister is able to list/get clusterversions and is populated by the shared
	// informer passed to NewClusterController.
	clusterVersionsLister colisters.ClusterVersionLister

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	// logger for controller
	logger log.FieldLogger
}

func (c *Controller) addClusterDeployment(obj interface{}) {
	clusterDeployment := obj.(*cov1.ClusterDeployment)
	cologging.WithClusterDeployment(c.logger, clusterDeployment).Debugf("adding cluster deployment")
	c.enqueueClusterDeployment(clusterDeployment)
}

func (c *Controller) updateClusterDeployment(old, cur interface{}) {
	oldClusterDeployment := old.(*cov1.ClusterDeployment)
	curClusterDeployment := cur.(*cov1.ClusterDeployment)
	cologging.WithClusterDeployment(c.logger, oldClusterDeployment).Debugf("updating cluster deployment")
	c.enqueueClusterDeployment(curClusterDeployment)
}

func (c *Controller) deleteClusterDeployment(obj interface{}) {
	clusterDeployment, ok := obj.(*cov1.ClusterDeployment)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		clusterDeployment, ok = tombstone.Obj.(*cov1.ClusterDeployment)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster Deployment %#v", obj))
			return
		}
	}
	cologging.WithClusterDeployment(c.logger, clusterDeployment).Debugf("deleting cluster deployment")
	c.enqueueClusterDeployment(clusterDeployment)
}

// When a cluster-api cluster is created, enqueue the cluster deployment that owns it and update its expectations.
func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*capiv1.Cluster)

	if cluster.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a cluster shows up in a state that
		// is already pending deletion. Prevent the cluster from being a creation observation.
		c.deleteCluster(cluster)
		return
	}

	clusterDeployment, err := cocontroller.ClusterDeploymentForCluster(cluster, c.clusterDeploymentsLister)
	if err != nil {
		cologging.WithCluster(c.logger, cluster).Errorf("error retrieving cluster deployment for cluster: %v", err)
		return
	}
	if clusterDeployment == nil {
		cologging.WithCluster(c.logger, cluster).Debugf("cluster is not controlled by a cluster deployment")
		return
	}
	cologging.WithCluster(cologging.WithClusterDeployment(c.logger, clusterDeployment), cluster).Debugln("cluster created")
	c.enqueueClusterDeployment(clusterDeployment)
}

// When a cluster is updated, figure out what cluster deployment manages it and wake it up.
func (c *Controller) updateCluster(old, cur interface{}) {
	oldCluster := old.(*capiv1.Cluster)
	curCluster := cur.(*capiv1.Cluster)
	if curCluster.ResourceVersion == oldCluster.ResourceVersion {
		// Periodic resync will send update events for all known clusters.
		// Two different versions of the same cluster will always have different RVs.
		return
	}

	if curCluster.DeletionTimestamp != nil {
		c.deleteCluster(curCluster)
		return
	}

	clusterDeployment, err := cocontroller.ClusterDeploymentForCluster(curCluster, c.clusterDeploymentsLister)
	if err != nil {
		cologging.WithCluster(c.logger, curCluster).Errorf("error retrieving cluster deployment for cluster: %v", err)
		return
	}
	if clusterDeployment == nil {
		cologging.WithCluster(c.logger, curCluster).Debugf("cluster is not controlled by a cluster deployment")
		return
	}
	cologging.WithCluster(cologging.WithClusterDeployment(c.logger, clusterDeployment), curCluster).Debugln("cluster updated")
	c.enqueueClusterDeployment(clusterDeployment)
}

// When a cluster is deleted, enqueue the cluster deployment that manages it and update its expectations.
func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*capiv1.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*capiv1.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}

	clusterDeployment, err := cocontroller.ClusterDeploymentForCluster(cluster, c.clusterDeploymentsLister)
	if err != nil {
		cologging.WithCluster(c.logger, cluster).Errorf("error retrieving cluster deployment for cluster: %v", err)
		return
	}
	if clusterDeployment == nil {
		cologging.WithCluster(c.logger, cluster).Debugf("cluster is not controlled by a cluster deployment")
		return
	}
	cologging.WithCluster(cologging.WithClusterDeployment(c.logger, clusterDeployment), cluster).Debugln("cluster deleted")
	c.enqueueClusterDeployment(clusterDeployment)
}

// When a machine set is created, enqueue the cluster deployment that manages it and update its expectations.
func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(*capiv1.MachineSet)

	if machineSet.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a new machine set shows up in a state that
		// is already pending deletion. Prevent the machine set from being a creation observation.
		c.deleteMachineSet(machineSet)
		return
	}

	clusterDeployment, err := cocontroller.ClusterDeploymentForMachineSet(machineSet, c.clusterDeploymentsLister)
	if err != nil {
		cologging.WithMachineSet(c.logger, machineSet).Errorf("error retrieving cluster deployment for machine set: %v", err)
		return
	}
	if clusterDeployment == nil {
		cologging.WithMachineSet(c.logger, machineSet).Debugf("machine set is not controlled by a cluster deployment")
		return
	}

	cologging.WithMachineSet(cologging.WithClusterDeployment(c.logger, clusterDeployment), machineSet).Debugln("machineset created")
	c.enqueueClusterDeployment(clusterDeployment)
}

// When a machine set is updated, figure out what cluster deployment manages it and wake it up.
func (c *Controller) updateMachineSet(old, cur interface{}) {
	oldMachineSet := old.(*capiv1.MachineSet)
	curMachineSet := cur.(*capiv1.MachineSet)
	if curMachineSet.ResourceVersion == oldMachineSet.ResourceVersion {
		// Periodic resync will send update events for all known machine sets.
		// Two different versions of the same machine set will always have different RVs.
		return
	}

	if curMachineSet.DeletionTimestamp != nil {
		c.deleteMachineSet(curMachineSet)
		return
	}

	clusterDeployment, err := cocontroller.ClusterDeploymentForMachineSet(curMachineSet, c.clusterDeploymentsLister)
	if err != nil {
		cologging.WithMachineSet(c.logger, curMachineSet).Errorf("error retrieving cluster deployment for machine set: %v", err)
		return
	}
	if clusterDeployment == nil {
		cologging.WithMachineSet(c.logger, curMachineSet).Debugf("machine set is not controlled by a cluster deployment")
		return
	}

	cologging.WithMachineSet(cologging.WithClusterDeployment(c.logger, clusterDeployment), curMachineSet).Debugln("machineset updated")
	c.enqueueClusterDeployment(clusterDeployment)
}

// When a machine set is deleted, enqueue the cluster that manages the machine set and update its expectations.
func (c *Controller) deleteMachineSet(obj interface{}) {
	machineSet, ok := obj.(*capiv1.MachineSet)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		machineSet, ok = tombstone.Obj.(*capiv1.MachineSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a MachineSet %#v", obj))
			return
		}
	}

	clusterDeployment, err := cocontroller.ClusterDeploymentForMachineSet(machineSet, c.clusterDeploymentsLister)
	if err != nil {
		cologging.WithMachineSet(c.logger, machineSet).Errorf("error retrieving cluster deployment for machine set: %v", err)
		return
	}
	if clusterDeployment == nil {
		cologging.WithMachineSet(c.logger, machineSet).Debugf("machine set is not controlled by a cluster deployment")
		return
	}

	cologging.WithMachineSet(cologging.WithClusterDeployment(c.logger, clusterDeployment), machineSet).Debugln("machineset deleted")
	c.enqueueClusterDeployment(clusterDeployment)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many cluster deployments will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Info("starting clusterdeployment controller")
	defer c.logger.Info("shutting down clusterdeployment controller")

	if !cocontroller.WaitForCacheSync("clusterdeployment", stopCh, c.clusterDeploymentsSynced, c.clustersSynced, c.machineSetsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(clusterDeployment *cov1.ClusterDeployment) {
	key, err := cocontroller.KeyFunc(clusterDeployment)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", clusterDeployment, err))
		return
	}

	c.queue.Add(key)
}

// enqueueAfter will enqueue a cluster deployment after the provided amount of time.
func (c *Controller) enqueueAfter(clusterDeployment *cov1.ClusterDeployment, after time.Duration) {
	key, err := cocontroller.KeyFunc(clusterDeployment)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", clusterDeployment, err))
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
	if err == nil {
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("Sync %q failed with %v", key, err))
	c.queue.AddRateLimited(key)

	return true
}

// syncClusterDeployment will sync the cluster with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *Controller) syncClusterDeployment(key string) error {
	startTime := time.Now()
	c.logger.WithField("key", key).Debug("syncing cluster deployment")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing cluster deployment")
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	clusterDeployment, err := c.clusterDeploymentsLister.ClusterDeployments(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Debug("cluster deployment has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	clusterDeploymentLog := cologging.WithClusterDeployment(c.logger, clusterDeployment)

	if clusterDeployment.DeletionTimestamp != nil {
		if !hasClusterDeploymentFinalizer(clusterDeployment) {
			return nil
		}
		return c.syncDeletedClusterDeployment(clusterDeployment, clusterDeploymentLog)
	}

	// Check for the delete-after annotation, and if the cluster has expired, delete it:
	deleteAfter, ok := clusterDeployment.Annotations[deleteAfterAnnotation]
	if ok {
		clusterDeploymentLog.Debugf("found delete after annotation: %s", deleteAfter)
		dur, err := time.ParseDuration(deleteAfter)
		if err != nil {
			return fmt.Errorf("error parsing %s as a duration: %v", deleteAfterAnnotation, err)
		}
		if !clusterDeployment.CreationTimestamp.IsZero() {
			expiry := clusterDeployment.CreationTimestamp.Add(dur)
			clusterDeploymentLog.Debugf("cluster expires at: %s", expiry)
			if time.Now().After(expiry) {
				clusterDeploymentLog.WithField("expiry", expiry).Info("cluster has expired, issuing delete")
				c.clustopClient.ClusteroperatorV1alpha1().ClusterDeployments(clusterDeployment.Namespace).Delete(clusterDeployment.Name, &metav1.DeleteOptions{})
				return nil
			}

			// We have an expiry time, we're not expired yet, lets make sure to enqueue the cluster for
			// just after its expiry time:
			enqueueDur := expiry.Sub(time.Now()) + 60*time.Second
			clusterDeploymentLog.Debugf("cluster will re-sync due to expiry time in: %v", enqueueDur)
			c.enqueueAfter(clusterDeployment, enqueueDur)
		}
	}

	if !hasClusterDeploymentFinalizer(clusterDeployment) {
		clusterDeploymentLog.Debugf("adding clusterdeployment finalizer")
		return c.addFinalizer(clusterDeployment)
	}

	// Only attempt to manage clusterdeployments if the version they should run is fully resolvable
	updatedClusterDeployment := clusterDeployment.DeepCopy()
	clusterVersion, err := c.getClusterVersion(clusterDeployment)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	versionMissing := clusterVersion == nil
	c.setMissingClusterVersionStatus(updatedClusterDeployment, versionMissing)
	if versionMissing {
		return c.updateClusterDeploymentStatus(clusterDeployment, updatedClusterDeployment)
	}

	if clusterDeployment.Spec.Hardware.AWS == nil {
		utilruntime.HandleError(fmt.Errorf("AWS hardware is not specified in clusterdeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name))
		return nil
	}

	if clusterVersion.Spec.VMImages.AWSImages == nil {
		utilruntime.HandleError(fmt.Errorf("No AWS images specified in clusterVersion %s/%s", clusterVersion.Namespace, clusterVersion.Name))
		return nil
	}

	validAWSRegion := c.validateAWSRegion(clusterDeployment, clusterVersion, clusterDeploymentLog)
	c.setMissingRegionStatus(updatedClusterDeployment, clusterVersion, !validAWSRegion)
	if !validAWSRegion {
		return c.updateClusterDeploymentStatus(clusterDeployment, updatedClusterDeployment)
	}

	cluster, err := c.syncCluster(updatedClusterDeployment, clusterVersion, clusterDeploymentLog)
	if err != nil {
		return err
	}

	err = c.syncControlPlane(updatedClusterDeployment, cluster, clusterVersion, clusterDeploymentLog)
	if err != nil {
		return err
	}

	return c.updateClusterDeploymentStatus(clusterDeployment, updatedClusterDeployment)
}

// updateClusterDeploymentStatus updates the status of the cluster deployment
func (c *Controller) updateClusterDeploymentStatus(original, clusterDeployment *cov1.ClusterDeployment) error {
	return cocontroller.PatchClusterDeploymentStatus(c.clustopClient, original, clusterDeployment)
}

func (c *Controller) syncDeletedClusterDeployment(clusterDeployment *cov1.ClusterDeployment, clusterDeploymentLog log.FieldLogger) error {
	// If remote machinesets have not been removed, wait until they are
	// When the cluster deployment is updated to remove them, it will be queued again.
	if hasRemoteMachineSetsFinalizer(clusterDeployment) {
		clusterDeploymentLog.Debugf("clusterdeployment still has remote machinesets finalizer, will wait until that is removed.")
		return nil
	}
	// Ensure that the master machineset is deleted
	machineSetName := masterMachineSetName(clusterDeployment.Spec.ClusterName)
	machineSet, err := c.machineSetsLister.MachineSets(clusterDeployment.Namespace).Get(machineSetName)

	// If there's an arbitrary error retrieving the machineset, return the error and retry
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error retrieving master machineset %s/%s: %v", clusterDeployment.Namespace, machineSetName, err)
	}
	// If the machineSet was found, but its DeletionTimestamp is already set, return and wait
	// for it to actually go away.
	if err == nil && machineSet.DeletionTimestamp != nil {
		cologging.WithMachineSet(clusterDeploymentLog, machineSet).Debugf("master machineset has been deleted, waiting until it goes away")
		return nil
	}
	// If the DeletionTimestamp is not set, then delete the machineset
	if err == nil {
		cologging.WithMachineSet(clusterDeploymentLog, machineSet).Infof("deleting master machineset")
		return c.capiClient.ClusterV1alpha1().MachineSets(clusterDeployment.Namespace).Delete(machineSetName, &metav1.DeleteOptions{})
	}

	// If we've reached this point, the master machineset no longer exists, clean up the cluster
	cluster, err := c.clustersLister.Clusters(clusterDeployment.Namespace).Get(clusterDeployment.Spec.ClusterName)

	// If there's an arbitrary error retrieving the cluster, return the error and retry
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error retrieving the cluster %s/%s: %v", clusterDeployment.Namespace, clusterDeployment.Spec.ClusterName, err)
	}

	if cluster != nil {
		// Upstream cluster-api automatically adds a finalizer to all Clusters in etcd, but it is only removed
		// in their cluster controller, which we do not use. Ensure this finalizer is removed from clusters when
		// a cluster deployment has been deleted.
		err = c.deleteClusterFinalizer(cluster)
		if err != nil {
			return err
		}
	}

	// If the cluster was found, but its DeletionTimestamp is already set, return and wait
	// for it to actually go away.
	if err == nil && cluster.DeletionTimestamp != nil {
		cologging.WithCluster(clusterDeploymentLog, cluster).Debugf("cluster has been deleted, waiting until it goes away")
		return nil
	}
	// If the DeletionTimestamp is not set, then delete the cluster
	if err == nil {
		cologging.WithCluster(clusterDeploymentLog, cluster).Debugf("deleting cluster")
		return c.capiClient.ClusterV1alpha1().Clusters(clusterDeployment.Namespace).Delete(clusterDeployment.Spec.ClusterName, &metav1.DeleteOptions{})
	}

	// If we've reached this point, the cluster no longer exists, remove the cluster deployment finalizer
	clusterDeploymentLog.Debugf("Dependent objects have been deleted. Removing finalizer")
	return c.deleteFinalizer(clusterDeployment)
}

// syncCluster takes a cluster deployment and ensures that a corresponding cluster exists and that
// it reflects the spec of the cluster deployment
func (c *Controller) syncCluster(clusterDeployment *cov1.ClusterDeployment, cv *cov1.ClusterVersion, logger log.FieldLogger) (*capiv1.Cluster, error) {
	cluster, err := c.clustersLister.Clusters(clusterDeployment.Namespace).Get(clusterDeployment.Spec.ClusterName)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("cannot retrieve cluster for cluster deployment %s/%s: %v", clusterDeployment.Namespace, clusterDeployment.Name, err)
	}

	if cluster == nil {
		cluster, err = cocontroller.BuildCluster(clusterDeployment, cv.Spec)
		if err != nil {
			return nil, fmt.Errorf("cannot build cluster for cluster deployment %s/%s: %v", clusterDeployment.Namespace, clusterDeployment.Name, err)
		}
		cluster, err := c.capiClient.ClusterV1alpha1().Clusters(clusterDeployment.Namespace).Create(cluster)
		if err != nil {
			return nil, fmt.Errorf("error creating cluster for cluster deployment %s/%s: %v", clusterDeployment.Namespace, clusterDeployment.Name, err)
		}
		return cluster, nil
	}

	// cluster exists, make sure it reflects the current cluster deployment spec
	providerConfig, err := cocontroller.BuildAWSClusterProviderConfig(&clusterDeployment.Spec, cv.Spec)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize provider config from existing cluster %s/%s: %v", cluster.Namespace, cluster.Name, err)
	}
	if !bytes.Equal(cluster.Spec.ProviderConfig.Value.Raw, providerConfig.Raw) {
		logger.Infof("cluster spec has changed, updating")
		updatedCluster := cluster.DeepCopy()
		updatedCluster.Spec.ProviderConfig.Value = providerConfig
		updatedCluster, err = c.capiClient.ClusterV1alpha1().Clusters(updatedCluster.Namespace).Update(updatedCluster)
		if err != nil {
			return nil, fmt.Errorf("cannot update existing cluster %s/%s: %v", cluster.Namespace, cluster.Name, err)
		}
		return updatedCluster, err
	}

	return cluster, nil
}

// syncControlPlane takes a cluster deployment and ensures that a corresponding master machine set
// exists and that its spec reflects the spec of the master machineset in the cluster deployment spec.
func (c *Controller) syncControlPlane(clusterDeployment *cov1.ClusterDeployment, cluster *capiv1.Cluster, clusterVersion *cov1.ClusterVersion, logger log.FieldLogger) error {
	machineSetName := masterMachineSetName(cluster.Name)
	machineSet, err := c.machineSetsLister.MachineSets(clusterDeployment.Namespace).Get(machineSetName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error retrieving master machineset %s/%s: %v", clusterDeployment.Namespace, machineSetName, err)
	}

	if machineSet == nil {
		clusterStatus, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
		if err != nil {
			return fmt.Errorf("cannot obtain cluster deployment status from cluster resource %s/%s: %v", cluster.Namespace, cluster.Name, err)
		}
		// If the cluster is not yet provisioned, do not attempt to create the master machineset.
		// Once the status is updated on the cluster resource, the corresponding cluster deployment
		// should be queued, therefore no need to retun an error to retry.
		if !clusterStatus.Provisioned {
			return nil
		}

		// machine set does not exist, it needs to be created
		machineSet, err = buildMasterMachineSet(clusterDeployment, cluster, clusterVersion)
		cologging.WithMachineSet(logger, machineSet).Debugf("About to create machineset")
		if err != nil {
			return fmt.Errorf("error building machineSet from clusterDeployment %s/%s: %v", clusterDeployment.Namespace, clusterDeployment.Name, err)
		}
		_, err = c.capiClient.ClusterV1alpha1().MachineSets(clusterDeployment.Namespace).Create(machineSet)
		if err != nil {
			return fmt.Errorf("error creating machineSet %s/%s: %v", machineSet.Namespace, machineSet.Name, err)
		}
		return nil
	}

	// master machine set exists, make sure it reflects the current cluster deployment master machine set spec
	machineSetConfig, ok := masterMachineSetConfig(clusterDeployment)
	if !ok {
		return fmt.Errorf("cluster deployment %s/%s does not have a master machineSetConfig", clusterDeployment.Namespace, clusterDeployment.Name)
	}

	needsUpdate := false
	updatedMachineSet := machineSet.DeepCopy()

	machineSetSize := int32(machineSetConfig.Size)
	if machineSet.Spec.Replicas == nil || *machineSet.Spec.Replicas != machineSetSize {
		needsUpdate = true
		updatedMachineSet.Spec.Replicas = &machineSetSize
	}

	// Build a new set of expected labels so we clear out any previous:
	newLabels := map[string]string{
		cov1.ClusterDeploymentLabel: clusterDeployment.Name,
		cov1.ClusterNameLabel:       cluster.Name,
		cov1.MachineSetNameLabel:    machineSet.Name,
	}
	for k, v := range machineSetConfig.NodeLabels {
		newLabels[k] = v
	}
	updatedMachineSet.Spec.Template.Spec.Labels = newLabels

	updatedMachineSet.Spec.Template.Spec.Taints = machineSetConfig.NodeTaints

	specProviderConfig, err := cocontroller.MachineProviderConfigFromMachineSetConfig(machineSetConfig, &clusterDeployment.Spec, clusterVersion)
	if err != nil {
		return fmt.Errorf("cannot create a machine providerconfig from machineset config for cluster deployment %s/%s: %v", clusterDeployment.Namespace, clusterDeployment.Name, err)
	}
	if !bytes.Equal(machineSet.Spec.Template.Spec.ProviderConfig.Value.Raw, specProviderConfig.Raw) {
		logger.Infof("master machineset config has changed, updating")
		needsUpdate = true
		updatedMachineSet.Spec.Template.Spec.ProviderConfig.Value = specProviderConfig
	}
	if needsUpdate {
		_, err := c.capiClient.ClusterV1alpha1().MachineSets(updatedMachineSet.Namespace).Update(updatedMachineSet)
		if err != nil {
			return fmt.Errorf("error updating machineset %s/%s: %v", machineSet.Namespace, machineSet.Name, err)
		}
	}
	return nil
}

func buildMasterMachineSet(clusterDeployment *cov1.ClusterDeployment, cluster *capiv1.Cluster, clusterVersion *cov1.ClusterVersion) (*capiv1.MachineSet, error) {
	machineSetConfig, ok := masterMachineSetConfig(clusterDeployment)
	if !ok {
		return nil, fmt.Errorf("cluster deployment %s/%s does not have a master machine set config", clusterDeployment.Namespace, clusterDeployment.Name)
	}

	machineSet := &capiv1.MachineSet{}
	machineSet.Name = masterMachineSetName(cluster.Name)
	machineSet.Namespace = clusterDeployment.Namespace
	machineSet.Labels = clusterDeployment.Labels
	if machineSet.Labels == nil {
		machineSet.Labels = make(map[string]string)
	}
	machineSet.Labels[cov1.ClusterDeploymentLabel] = clusterDeployment.Name
	machineSet.Labels[cov1.ClusterNameLabel] = cluster.Name
	for k, v := range machineSetConfig.NodeLabels {
		machineSet.Labels[k] = v
	}
	blockOwnerDeletion := false
	ownerRef := metav1.NewControllerRef(clusterDeployment, cocontroller.ClusterDeploymentKind)
	ownerRef.BlockOwnerDeletion = &blockOwnerDeletion
	machineSet.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	machineSetLabels := map[string]string{
		cov1.MachineSetNameLabel:    machineSet.Name,
		cov1.ClusterDeploymentLabel: clusterDeployment.Name,
		cov1.ClusterNameLabel:       cluster.Name,
	}
	machineSet.Spec.Selector.MatchLabels = machineSetLabels
	replicas := int32(machineSetConfig.Size)
	machineSet.Spec.Replicas = &replicas
	machineSet.Spec.Template.Labels = machineSetLabels

	// Transfer node labels onto the machine template:
	machineSet.Spec.Template.Spec.Labels = map[string]string{}
	for k, v := range machineSetLabels {
		machineSet.Spec.Template.Spec.Labels[k] = v
	}
	for k, v := range machineSetConfig.NodeLabels {
		machineSet.Spec.Template.Spec.Labels[k] = v
	}
	machineSet.Spec.Template.Spec.Taints = machineSetConfig.NodeTaints

	providerConfig, err := cocontroller.MachineProviderConfigFromMachineSetConfig(machineSetConfig, &clusterDeployment.Spec, clusterVersion)
	if err != nil {
		return nil, err
	}
	machineSet.Spec.Template.Spec.ProviderConfig.Value = providerConfig
	return machineSet, nil
}

// validateAWSRegion will check that the cluster's version has an AMI defined for its region. If not, an error will be returned.
func (c *Controller) validateAWSRegion(clusterDeployment *cov1.ClusterDeployment, clusterVersion *cov1.ClusterVersion, clusterDeploymentLog log.FieldLogger) bool {
	// Make sure the cluster version supports the region for the clusterDeployment, if not return an error
	foundRegion := false
	for _, regionAMI := range clusterVersion.Spec.VMImages.AWSImages.RegionAMIs {
		if regionAMI.Region == clusterDeployment.Spec.Hardware.AWS.Region {
			foundRegion = true
		}
	}

	if !foundRegion {
		clusterDeploymentLog.Warnf("no AMI defined for cluster version %s/%s in region %s", clusterVersion.Namespace, clusterVersion.Name, clusterDeployment.Spec.Hardware.AWS.Region)
	}
	return foundRegion
}

// setMissingClusterVersionStatus updates the cluster deployment status to indicate that the clusterVersion is
// present or missing
func (c *Controller) setMissingClusterVersionStatus(clusterDeployment *cov1.ClusterDeployment, missing bool) {
	clusterVersionRef := clusterDeployment.Spec.ClusterVersionRef
	var (
		msg, reason string
		status      corev1.ConditionStatus
		updateCheck cocontroller.UpdateConditionCheck
	)
	if missing {
		msg = fmt.Sprintf("cluster version %s/%s was not found", clusterVersionRef.Namespace, clusterVersionRef.Name)
		status = corev1.ConditionTrue
		updateCheck = cocontroller.UpdateConditionIfReasonOrMessageChange
		reason = versionMissing
	} else {
		msg = fmt.Sprintf("cluster version %s/%s was found", clusterVersionRef.Namespace, clusterVersionRef.Name)
		status = corev1.ConditionFalse
		updateCheck = cocontroller.UpdateConditionNever
		reason = versionExists
	}
	clusterDeployment.Status.Conditions = cocontroller.SetClusterDeploymentCondition(clusterDeployment.Status.Conditions, cov1.ClusterVersionMissing, status, reason, msg, updateCheck)
}

// setMissingRegionStatus updates the cluster deployment status to indicate that the clusterVersion does not include an AMI for the region of
// the cluster deployment
func (c *Controller) setMissingRegionStatus(clusterDeployment *cov1.ClusterDeployment, clusterVersion *cov1.ClusterVersion, missing bool) {
	var (
		msg, reason string
		status      corev1.ConditionStatus
		updateCheck cocontroller.UpdateConditionCheck
	)
	if missing {
		msg = fmt.Sprintf("no AMI defined for cluster version %s/%s in region %v", clusterVersion.Namespace, clusterVersion.Name, clusterDeployment.Spec.Hardware.AWS.Region)
		status = corev1.ConditionTrue
		updateCheck = cocontroller.UpdateConditionIfReasonOrMessageChange
		reason = versionMissingRegion
	} else {
		msg = fmt.Sprintf("AMI defined for cluster version %s/%s in region %v", clusterVersion.Namespace, clusterVersion.Name, clusterDeployment.Spec.Hardware.AWS.Region)
		status = corev1.ConditionFalse
		updateCheck = cocontroller.UpdateConditionNever
		reason = versionHasRegion
	}

	clusterDeployment.Status.Conditions = cocontroller.SetClusterDeploymentCondition(clusterDeployment.Status.Conditions, cov1.ClusterVersionIncompatible, status, reason, msg, updateCheck)
}

// getClusterVersion retrieves the cluster version referenced by the cluster deployment.
func (c *Controller) getClusterVersion(clusterDeployment *cov1.ClusterDeployment) (*cov1.ClusterVersion, error) {
	// Namespace may have been left empty signalling to use the clusterDeployment's namespace to locate the version:
	clusterVersionRef := clusterDeployment.Spec.ClusterVersionRef
	versionNS := clusterVersionRef.Namespace
	if versionNS == "" {
		versionNS = clusterDeployment.Namespace
	}
	return c.clusterVersionsLister.ClusterVersions(versionNS).Get(clusterVersionRef.Name)
}

func masterMachineSetName(clusterName string) string {
	return fmt.Sprintf("%s-%s", clusterName, cov1.MasterMachineSetName)
}

// masterMachineSetConfig finds the MachineSetConfig in a cluster deployment spec with node type Master.
// Returns the MachineSetConfig and a boolean indicating whether it was found.
func masterMachineSetConfig(clusterDeployment *cov1.ClusterDeployment) (*cov1.MachineSetConfig, bool) {
	for _, ms := range clusterDeployment.Spec.MachineSets {
		if ms.NodeType == cov1.NodeTypeMaster {
			return &ms.MachineSetConfig, true
		}
	}
	return nil, false
}

func hasClusterDeploymentFinalizer(clusterDeployment *cov1.ClusterDeployment) bool {
	return cocontroller.HasFinalizer(clusterDeployment, cov1.FinalizerClusterDeployment)
}

func hasRemoteMachineSetsFinalizer(clusterDeployment *cov1.ClusterDeployment) bool {
	return cocontroller.HasFinalizer(clusterDeployment, cov1.FinalizerRemoteMachineSets)
}

func (c *Controller) deleteFinalizer(clusterDeployment *cov1.ClusterDeployment) error {
	clusterDeployment = clusterDeployment.DeepCopy()
	cocontroller.DeleteFinalizer(clusterDeployment, cov1.FinalizerClusterDeployment)
	_, err := c.clustopClient.ClusteroperatorV1alpha1().ClusterDeployments(clusterDeployment.Namespace).UpdateStatus(clusterDeployment)
	return err
}

func (c *Controller) deleteClusterFinalizer(cluster *capiv1.Cluster) error {
	cluster = cluster.DeepCopy()
	cocontroller.DeleteFinalizer(cluster, capiv1.ClusterFinalizer)
	_, err := c.capiClient.ClusterV1alpha1().Clusters(cluster.Namespace).UpdateStatus(cluster)
	return err
}

func (c *Controller) addFinalizer(clusterDeployment *cov1.ClusterDeployment) error {
	clusterDeployment = clusterDeployment.DeepCopy()
	cocontroller.AddFinalizer(clusterDeployment, cov1.FinalizerClusterDeployment)
	_, err := c.clustopClient.ClusteroperatorV1alpha1().ClusterDeployments(clusterDeployment.Namespace).UpdateStatus(clusterDeployment)
	return err
}
