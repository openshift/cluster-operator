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

package cluster

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	colog "github.com/openshift/cluster-operator/pkg/logging"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = clusteroperator.SchemeGroupVersion.WithKind("Cluster")

const (
	controllerLogName = "cluster"
)

// NewController returns a new controller.
func NewController(clusterInformer informers.ClusterInformer, machineSetInformer informers.MachineSetInformer, clusterVersionInformer informers.ClusterVersionInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_cluster_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", controllerLogName)
	c := &Controller{
		client:       clusteroperatorClient,
		expectations: controller.NewUIDTrackingExpectations(controller.NewExpectations()),
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),
		logger:       logger,
	}

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

	c.syncHandler = c.syncCluster
	c.enqueueCluster = c.enqueue

	return c
}

// Controller manages clusters.
type Controller struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueCluster func(cluster *clusteroperator.Cluster)

	// A TTLCache of machine set creates/deletes each cluster expects to see.
	expectations *controller.UIDTrackingExpectations

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// clusterVersionsLister is able to list/get clusterversions and is populated by the shared
	// informer passed to NewClusterController.
	clusterVersionsLister lister.ClusterVersionLister

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	colog.WithCluster(c.logger, cluster).Debugf("adding cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) updateCluster(old, cur interface{}) {
	oldCluster := old.(*clusteroperator.Cluster)
	curCluster := cur.(*clusteroperator.Cluster)
	colog.WithCluster(c.logger, oldCluster).Debugf("updating cluster")
	c.enqueueCluster(curCluster)
}

func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*clusteroperator.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*clusteroperator.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	colog.WithCluster(c.logger, cluster).Debugf("deleting cluster")
	c.enqueueCluster(cluster)
}

// When a machine set is created, enqueue the cluster that manages it and update its expectations.
func (c *Controller) addMachineSet(obj interface{}) {
	machineSet := obj.(*clusteroperator.MachineSet)

	if machineSet.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a new machine set shows up in a state that
		// is already pending deletion. Prevent the machine set from being a creation observation.
		c.deleteMachineSet(machineSet)
		return
	}

	cluster, err := controller.ClusterForMachineSet(machineSet, c.clustersLister)
	if err != nil {
		glog.V(2).Infof("error retrieving cluster for machine set %q/%q: %v", machineSet.Namespace, machineSet.Name, err)
		return
	}
	if cluster == nil {
		glog.V(6).Infof("machine set %q/%q added that is not controlled by a cluster", machineSet.Namespace, machineSet.Name)
		return
	}

	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		return
	}
	colog.WithMachineSet(colog.WithCluster(c.logger, cluster), machineSet).Debugln("machineset created")
	c.expectations.CreationObserved(clusterKey)
	c.enqueueCluster(cluster)
}

// When a machine set is updated, figure out what cluster manages it and wake it
// up.
func (c *Controller) updateMachineSet(old, cur interface{}) {
	oldMachineSet := old.(*clusteroperator.MachineSet)
	curMachineSet := cur.(*clusteroperator.MachineSet)
	if curMachineSet.ResourceVersion == oldMachineSet.ResourceVersion {
		// Periodic resync will send update events for all known machine sets.
		// Two different versions of the same machine set will always have different RVs.
		return
	}

	if curMachineSet.DeletionTimestamp != nil {
		// when a machine set is deleted gracefully it's deletion timestamp is first modified to reflect a grace period,
		// and after such time has passed, the kubelet actually deletes it from the store. We receive an update
		// for modification of the deletion timestamp and expect a cluster to create a replacement machine set asap, not wait
		// until the kubelet actually deletes the machine set. This is different from the Phase of a machine set changing, because
		// a cluster never initiates a phase change, and so is never asleep waiting for the same.
		c.deleteMachineSet(curMachineSet)
		return
	}

	cluster, err := controller.ClusterForMachineSet(curMachineSet, c.clustersLister)
	if err != nil {
		glog.V(2).Infof("error retrieving cluster for machine set %q/%q: %v", curMachineSet.Namespace, curMachineSet.Name, err)
		return
	}
	if cluster == nil {
		glog.V(6).Infof("machine set %q/%q updated that is not controlled by a cluster", curMachineSet.Namespace, curMachineSet.Name)
		return
	}
	colog.WithMachineSet(colog.WithCluster(c.logger, cluster), curMachineSet).Debugf("machine set updated")
	c.enqueueCluster(cluster)
}

// When a machine set is deleted, enqueue the cluster that manages the machine set and update its expectations.
func (c *Controller) deleteMachineSet(obj interface{}) {
	machineSet, ok := obj.(*clusteroperator.MachineSet)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		machineSet, ok = tombstone.Obj.(*clusteroperator.MachineSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}

	cluster, err := controller.ClusterForMachineSet(machineSet, c.clustersLister)
	if err != nil {
		glog.V(2).Infof("error retrieving cluster for machine set %q/%q: %v", machineSet.Namespace, machineSet.Name, err)
		return
	}
	if cluster == nil {
		glog.V(6).Infof("machine set %q/%q deleted that is not controlled by a cluster", machineSet.Namespace, machineSet.Name)
		return
	}

	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		return
	}
	colog.WithMachineSet(colog.WithCluster(c.logger, cluster), machineSet).Debugf("machine set deleted")
	c.expectations.DeletionObserved(clusterKey, getMachineSetKey(machineSet))
	c.enqueueCluster(cluster)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Info("starting cluster controller")
	defer c.logger.Info("shutting down cluster controller")

	if !controller.WaitForCacheSync("cluster", stopCh, c.clustersSynced, c.machineSetsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(cluster *clusteroperator.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

// enqueueAfter will enqueue a cluster after the provided amount of time.
func (c *Controller) enqueueAfter(cluster *clusteroperator.Cluster, after time.Duration) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
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

// syncCluster will sync the cluster with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *Controller) syncCluster(key string) error {
	startTime := time.Now()
	c.logger.WithField("key", key).Debug("syncing cluster")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing cluster")
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	cluster, err := c.clustersLister.Clusters(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Debug("cluster has been deleted")
		c.expectations.DeleteExpectations(key)
		return nil
	}
	if err != nil {
		return err
	}
	clusterLog := colog.WithCluster(c.logger, cluster)

	clusterNeedsSync := c.expectations.SatisfiedExpectations(key)

	// List all active machine sets owned by this cluster
	allMachineSets, err := c.machineSetsLister.MachineSets(cluster.Namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	var filteredMachineSets []*clusteroperator.MachineSet
	for _, machineSet := range allMachineSets {
		if machineSet.DeletionTimestamp != nil {
			continue
		}
		controllerRef := metav1.GetControllerOf(machineSet)
		if controllerRef == nil {
			continue
		}
		if cluster.UID != controllerRef.UID {
			continue
		}
		filteredMachineSets = append(filteredMachineSets, machineSet)
	}

	var manageMachineSetsErr error

	// Only attempt to manage cluster machine sets if the version they should run is fully resolvable:
	clusterVersion, resolveCVErr := c.resolveClusterVersion(cluster)
	if errors.IsNotFound(resolveCVErr) {
		clusterLog.Debugf("cluster version %v does not yet exist, skipping machine set management and requeuing cluster",
			cluster.Spec.ClusterVersionRef)
		clusterVersion = nil
	} else if resolveCVErr != nil {
		clusterLog.Errorf("unexpected error looking up cluster version %v: %v",
			cluster.Spec.ClusterVersionRef, resolveCVErr)
		clusterVersion = nil
	} else if clusterNeedsSync && cluster.DeletionTimestamp == nil {
		manageMachineSetsErr = c.manageMachineSets(filteredMachineSets, cluster, clusterVersion)
	}

	original := cluster
	cluster = cluster.DeepCopy()

	// Despite some potential errors above, we still want to update status, then return the errors to have
	// the cluster re-queued.
	newStatus, updateStatusErr := c.calculateStatus(clusterLog, cluster, clusterVersion, filteredMachineSets)
	cluster.Status = newStatus

	if err := controller.PatchClusterStatus(c.client, original, cluster); err != nil {
		// Multiple things could lead to this update failing. Requeuing the cluster ensures
		// returning an error causes a requeue without forcing a hotloop
		return err
	}
	clusterLog.Debugf("updated status: %v", cluster.Status)

	if resolveCVErr != nil {
		if errors.IsNotFound(resolveCVErr) {
			c.enqueueAfter(cluster, 10*time.Second) // recheck for missing version every 10 seconds indefinitely
			return nil
		}
		return resolveCVErr
	} else if updateStatusErr != nil {
		return updateStatusErr
	} else if manageMachineSetsErr != nil {
		return manageMachineSetsErr
	}

	return nil
}

// manageMachineSets checks and updates machine sets for the given cluster.
// Does NOT modify <machineSets>.
// It will requeue the cluster in case of an error while creating/deleting machine sets.
func (c *Controller) manageMachineSets(machineSets []*clusteroperator.MachineSet, cluster *clusteroperator.Cluster, clusterVersion *clusteroperator.ClusterVersion) error {
	clusterLog := colog.WithCluster(c.logger, cluster)
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for Cluster %#v: %v", cluster, err))
		return nil
	}
	clusterLog.Debugf("managing machine sets")

	// This function should not be called with a nil clusterVersion as we don't want to do any machine set management
	// in that situation.
	if clusterVersion == nil {
		clusterLog.WithField("currentCount", len(machineSets)).Errorf("cannot manage machinesets when clusterversion unresolved: %v", cluster.Spec.ClusterVersionRef)
		return fmt.Errorf("cannot manage machinesets when clusterversion unresolved: %v", cluster.Spec.ClusterVersionRef)
	}

	machineSetPrefixes := make([]string, len(cluster.Spec.MachineSets))
	for i, machineSet := range cluster.Spec.MachineSets {
		machineSetPrefixes[i] = getNamePrefixForMachineSet(cluster, machineSet.Name)
	}

	clusterMachineSets := make([]*clusteroperator.MachineSet, len(cluster.Spec.MachineSets))
	machineSetsToCreate := []*clusteroperator.MachineSet{}
	machineSetsToDelete := []*clusteroperator.MachineSet{}

	// Organize machine sets
	for _, machineSet := range machineSets {
		found := false
		for i, prefix := range machineSetPrefixes {
			if strings.HasPrefix(machineSet.Name, prefix) {
				if clusterMachineSets[i] == nil {
					clusterMachineSets[i] = machineSet
				} else {
					utilruntime.HandleError(fmt.Errorf("Found two active conflicting machine sets for cluster %s/%s: %s and %s", cluster.Namespace, cluster.Name, clusterMachineSets[i].Name, machineSet.Name))
					machineSetsToDelete = append(machineSetsToDelete, machineSet)
				}
				found = true
			}
		}
		if !found {
			machineSetsToDelete = append(machineSetsToDelete, machineSet)
		}
	}

	errCh := make(chan error, len(cluster.Spec.MachineSets)+len(machineSetsToDelete))

	// Sync machine sets
	for i := range cluster.Spec.MachineSets {
		machineSetConfig := cluster.Spec.MachineSets[i].MachineSetConfig
		mergedHardwareSpec, err := applyDefaultMachineSetHardwareSpec(machineSetConfig.Hardware, cluster.Spec.DefaultHardwareSpec)
		if err != nil {
			errCh <- err
			continue
		}
		machineSetConfig.Hardware = mergedHardwareSpec

		machineSetToCreate, deleteMachineSet, err := c.manageMachineSet(cluster, clusterVersion, clusterMachineSets[i], machineSetConfig, machineSetPrefixes[i])
		if err != nil {
			errCh <- err
			continue
		}

		if machineSetToCreate != nil {
			machineSetsToCreate = append(machineSetsToCreate, machineSetToCreate)
		}
		if deleteMachineSet {
			machineSetsToDelete = append(machineSetsToDelete, clusterMachineSets[i])
		}
	}

	// Snapshot the UIDs (ns/name) of the machine sets we're expecting to see
	// deleted, so we know to record their expectations exactly once either
	// when we see it as an update of the deletion timestamp, or as a delete.
	deletedMachineSetKeys := make([]string, len(machineSetsToDelete))
	for i, ms := range machineSetsToDelete {
		deletedMachineSetKeys[i] = getMachineSetKey(ms)
	}
	if err := c.expectations.SetExpectations(clusterKey, len(machineSetsToCreate), deletedMachineSetKeys); err != nil {
		return err
	}

	if len(machineSetsToCreate) > 0 {
		var wg sync.WaitGroup
		clusterLog.Infof("creating %d new machine sets", len(machineSetsToCreate))
		wg.Add(len(machineSetsToCreate))
		for i, ng := range machineSetsToCreate {
			go func(ix int, machineSet *clusteroperator.MachineSet) {
				defer wg.Done()
				_, err := c.client.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).Create(machineSet)
				if err != nil && errors.IsTimeout(err) {
					// Machine set is created but its initialization has timed out.
					// If the initialization is successful eventually, the
					// controller will observe the creation via the informer.
					// If the initialization fails, or if the machine set stays
					// uninitialized for a long time, the informer will not
					// receive any update, and the controller will create a new
					// machine set when the expectation expires.
					return
				}
				if err != nil {
					// Decrement the expected number of creates because the informer won't observe this machine set
					clusterLog.Warnf("failed creation, decrementing expectations for cluster: %v", err)
					c.expectations.CreationObserved(clusterKey)
					errCh <- err
				} else {
					colog.WithMachineSet(clusterLog, machineSet).Info("created machine set")
				}
			}(i, ng)
		}
		wg.Wait()
	}

	if len(machineSetsToDelete) > 0 {
		var wg sync.WaitGroup
		wg.Add(len(machineSetsToDelete))
		for i, ng := range machineSetsToDelete {
			go func(ix int, machineSet *clusteroperator.MachineSet) {
				defer wg.Done()
				if err := c.client.ClusteroperatorV1alpha1().MachineSets(cluster.Namespace).Delete(machineSet.Name, &metav1.DeleteOptions{}); err != nil {
					// Decrement the expected number of deletes because the informer won't observe this deletion
					machineSetKey := deletedMachineSetKeys[ix]
					clusterLog.Errorf("Failed to delete %v, decrementing expectations for cluster", machineSetKey)
					c.expectations.DeletionObserved(clusterKey, machineSetKey)
					errCh <- err
				} else {
					colog.WithMachineSet(clusterLog, machineSet).Info("deleted machine set")
				}

			}(i, ng)
		}
		wg.Wait()
	}

	select {
	case err := <-errCh:
		// all errors have been reported before and they're likely to be the same, so we'll only return the first one we hit.
		if err != nil {
			return err
		}
	default:
	}
	return nil
}

// manageMachineSet determines whether or not a machine set needs to be created because it does not exist, or replaced because it is out of date.
func (c *Controller) manageMachineSet(cluster *clusteroperator.Cluster, clusterVersion *clusteroperator.ClusterVersion, machineSet *clusteroperator.MachineSet, clusterMachineSetConfig clusteroperator.MachineSetConfig, machineSetNamePrefix string) (*clusteroperator.MachineSet, bool, error) {
	clusterLog := colog.WithCluster(c.logger, cluster)
	if machineSet == nil {
		clusterLog.Debugf("building new machine set")
		machineSet, err := buildNewMachineSet(cluster, clusterVersion, clusterMachineSetConfig, machineSetNamePrefix)
		return machineSet, false, err
	}

	msLog := colog.WithMachineSet(clusterLog, machineSet)

	if !apiequality.Semantic.DeepEqual(machineSet.Spec.MachineSetConfig, clusterMachineSetConfig) {
		msLog.Infof("machine set configuration has changed from %v to %v",
			machineSet.Spec.MachineSetConfig, clusterMachineSetConfig)
		machineSet, err := buildNewMachineSet(cluster, clusterVersion, clusterMachineSetConfig, machineSetNamePrefix)
		return machineSet, true, err
	}

	if machineSet.Spec.ClusterVersionRef.UID != clusterVersion.UID {
		msLog.Infof("machine set cluster version has changed from %v to %s/%s",
			machineSet.Spec.ClusterVersionRef, clusterVersion.Namespace, clusterVersion.Name)
		machineSet, err := buildNewMachineSet(cluster, clusterVersion, clusterMachineSetConfig, machineSetNamePrefix)
		return machineSet, true, err
	}

	return nil, false, nil
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (c *Controller) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *clusteroperator.Cluster {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != controllerKind.Kind {
		return nil
	}
	cluster, err := c.clustersLister.Clusters(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if cluster.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return cluster
}

func (c *Controller) calculateStatus(clusterLog log.FieldLogger, cluster *clusteroperator.Cluster, resolvedClusterVersion *clusteroperator.ClusterVersion, machineSets []*clusteroperator.MachineSet) (clusteroperator.ClusterStatus, error) {
	newStatus := cluster.Status
	oldClusterVersion := cluster.Status.ClusterVersionRef

	newStatus.MachineSetCount = 0
	for _, ms := range cluster.Spec.MachineSets {
		if machineSet := findMachineSetWithPrefix(machineSets, getNamePrefixForMachineSet(cluster, ms.Name)); machineSet != nil {
			colog.WithMachineSet(clusterLog, machineSet).Debugf("machineset added to status.MachineSetCount")
			newStatus.MachineSetCount++
			if machineSet.Spec.NodeType == clusteroperator.NodeTypeMaster {
				newStatus.MasterMachineSetName = machineSet.Name
			}
			if machineSet.Spec.Infra {
				newStatus.InfraMachineSetName = machineSet.Name
			}
		}
	}

	if oldClusterVersion == nil {
		oldClusterVersion = &corev1.ObjectReference{}
	}

	if resolvedClusterVersion != nil &&
		resolvedClusterVersion.UID != oldClusterVersion.UID {

		clusterLog.Infof("cluster version has changed from %s/%s to %s/%s",
			oldClusterVersion.Namespace,
			oldClusterVersion.Name,
			resolvedClusterVersion.Namespace,
			resolvedClusterVersion.Name)
		newStatus.ClusterVersionRef = &corev1.ObjectReference{
			Name:      resolvedClusterVersion.Name,
			Namespace: resolvedClusterVersion.Namespace, // Namespace will always be resolved on the status+machine sets
			UID:       resolvedClusterVersion.UID,
		}
	}
	return newStatus, nil
}

// resolveClusterVersion checks if the cluster version referenced by the ClusterSpec exists and returns it. If not found or the lookup fails, an error is returned.
func (c *Controller) resolveClusterVersion(cluster *clusteroperator.Cluster) (*clusteroperator.ClusterVersion, error) {
	// Namespace may have been left empty signalling to use the cluster's namespace to locate the version:
	versionNS := cluster.Spec.ClusterVersionRef.Namespace
	if versionNS == "" {
		versionNS = cluster.Namespace
	}

	cv, err := c.clusterVersionsLister.ClusterVersions(versionNS).Get(
		cluster.Spec.ClusterVersionRef.Name)
	return cv, err
}

func applyDefaultMachineSetHardwareSpec(machineSetHardwareSpec, defaultHardwareSpec *clusteroperator.MachineSetHardwareSpec) (*clusteroperator.MachineSetHardwareSpec, error) {
	if defaultHardwareSpec == nil {
		return machineSetHardwareSpec, nil
	}
	defaultHwSpecJSON, err := json.Marshal(defaultHardwareSpec)
	if err != nil {
		return nil, err
	}
	specificHwSpecJSON, err := json.Marshal(machineSetHardwareSpec)
	if err != nil {
		return nil, err
	}
	merged, err := strategicpatch.StrategicMergePatch(defaultHwSpecJSON, specificHwSpecJSON, machineSetHardwareSpec)
	mergedSpec := &clusteroperator.MachineSetHardwareSpec{}
	if err = json.Unmarshal(merged, mergedSpec); err != nil {
		return nil, err
	}
	return mergedSpec, nil
}

func buildNewMachineSet(cluster *clusteroperator.Cluster, clusterVersion *clusteroperator.ClusterVersion, machineSetConfig clusteroperator.MachineSetConfig, machineSetNamePrefix string) (*clusteroperator.MachineSet, error) {
	return &clusteroperator.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    machineSetNamePrefix,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cluster, controllerKind)},
		},
		Spec: clusteroperator.MachineSetSpec{
			MachineSetConfig: machineSetConfig,
			ClusterVersionRef: corev1.ObjectReference{
				Name:      clusterVersion.Name,
				Namespace: clusterVersion.Namespace,
				UID:       clusterVersion.UID,
			},
		},
	}, nil
}

func getNamePrefixForMachineSet(cluster *clusteroperator.Cluster, name string) string {
	return fmt.Sprintf("%s-%s-", cluster.Name, name)
}

func findMachineSetWithPrefix(machineSets []*clusteroperator.MachineSet, prefix string) *clusteroperator.MachineSet {
	for _, machineSet := range machineSets {
		if strings.HasPrefix(machineSet.Name, prefix) {
			return machineSet
		}
	}
	return nil
}

func getMachineSetKey(machineSet *clusteroperator.MachineSet) string {
	return fmt.Sprintf("%s/%s", machineSet.Namespace, machineSet.Name)
}
