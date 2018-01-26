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

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = clusteroperator.SchemeGroupVersion.WithKind("Cluster")

// NewClusterController returns a new *ClusterController.
func NewClusterController(clusterInformer informers.ClusterInformer, machineSetInformer informers.MachineSetInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *ClusterController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_cluster_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &ClusterController{
		client:       clusteroperatorClient,
		expectations: controller.NewUIDTrackingControllerExpectations(controller.NewControllerExpectations()),
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),
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

	c.syncHandler = c.syncCluster
	c.enqueueCluster = c.enqueue

	return c
}

// ClusterController manages clusters.
type ClusterController struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueCluster func(cluster *clusteroperator.Cluster)

	// A TTLCache of machine set creates/deletes each cluster expects to see.
	expectations *controller.UIDTrackingControllerExpectations

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewClusterController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewClusterController.
	machineSetsLister lister.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *ClusterController) addCluster(obj interface{}) {
	cluster := obj.(*clusteroperator.Cluster)
	glog.V(4).Infof("Adding cluster %s", cluster.Name)
	c.enqueueCluster(cluster)
}

func (c *ClusterController) updateCluster(old, cur interface{}) {
	oldCluster := old.(*clusteroperator.Cluster)
	curCluster := cur.(*clusteroperator.Cluster)
	glog.V(4).Infof("Updating cluster %s", oldCluster.Name)
	c.enqueueCluster(curCluster)
}

func (c *ClusterController) deleteCluster(obj interface{}) {
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
	glog.V(4).Infof("Deleting cluster %s", cluster.Name)
	c.enqueueCluster(cluster)
}

// When a machine set is created, enqueue the cluster that manages it and update its expectations.
func (c *ClusterController) addMachineSet(obj interface{}) {
	machineSet := obj.(*clusteroperator.MachineSet)

	if machineSet.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a new machine set shows up in a state that
		// is already pending deletion. Prevent the machine set from being a creation observation.
		c.deleteMachineSet(machineSet)
		return
	}

	controllerRef := metav1.GetControllerOf(machineSet)
	if controllerRef == nil {
		return
	}
	cluster := c.resolveControllerRef(machineSet.Namespace, controllerRef)
	if cluster == nil {
		return
	}
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		return
	}
	glog.V(4).Infof("Machine set %s created: %#v.", machineSet.Name, machineSet)
	c.expectations.CreationObserved(clusterKey)
	c.enqueueCluster(cluster)
}

// When a machine set is updated, figure out what cluster manages it and wake it
// up.
func (c *ClusterController) updateMachineSet(old, cur interface{}) {
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

	controllerRef := metav1.GetControllerOf(curMachineSet)
	if controllerRef == nil {
		return
	}
	cluster := c.resolveControllerRef(curMachineSet.Namespace, controllerRef)
	if cluster == nil {
		return
	}
	glog.V(4).Infof("Machine set %s updated, objectMeta %+v -> %+v.", curMachineSet.Name, oldMachineSet.ObjectMeta, curMachineSet.ObjectMeta)
	c.enqueueCluster(cluster)
}

// When a machine set is deleted, enqueue the cluster that manages the machine set and update its expectations.
func (c *ClusterController) deleteMachineSet(obj interface{}) {
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

	controllerRef := metav1.GetControllerOf(machineSet)
	if controllerRef == nil {
		return
	}
	cluster := c.resolveControllerRef(machineSet.Namespace, controllerRef)
	if cluster == nil {
		return
	}
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		return
	}
	glog.V(4).Infof("Machine set %s/%s deleted through %v, timestamp %+v: %#v.", machineSet.Namespace, machineSet.Name, utilruntime.GetCaller(), machineSet.DeletionTimestamp, machineSet)
	c.expectations.DeletionObserved(clusterKey, getMachineSetKey(machineSet))
	c.enqueueCluster(cluster)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// clusters will be handled in parallel.
func (c *ClusterController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting cluster controller")
	defer glog.Infof("Shutting down cluster controller")

	if !controller.WaitForCacheSync("cluster", stopCh, c.clustersSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *ClusterController) enqueue(cluster *clusteroperator.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

func (c *ClusterController) enqueueRateLimited(cluster *clusteroperator.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a cluster after the provided amount of time.
func (c *ClusterController) enqueueAfter(cluster *clusteroperator.Cluster, after time.Duration) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *ClusterController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *ClusterController) processNextWorkItem() bool {
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
func (c *ClusterController) syncCluster(key string) error {
	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing cluster %q (%v)", key, time.Now().Sub(startTime))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	cluster, err := c.clustersLister.Clusters(namespace).Get(name)
	if errors.IsNotFound(err) {
		glog.V(4).Infof("Cluster has been deleted %v", key)
		c.expectations.DeleteExpectations(key)
		return nil
	}
	if err != nil {
		return err
	}

	clusterNeedsSync := c.expectations.SatisfiedExpectations(key)

	// List all active machine sets controller by the cluster
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
	if clusterNeedsSync && cluster.DeletionTimestamp == nil {
		manageMachineSetsErr = c.manageMachineSets(filteredMachineSets, cluster)
	}

	cluster = cluster.DeepCopy()

	newStatus := calculateStatus(cluster, filteredMachineSets, manageMachineSetsErr)

	// Always updates status as machine sets come up or die.
	if _, err := c.updateClusterStatus(cluster, newStatus); err != nil {
		// Multiple things could lead to this update failing. Requeuing the cluster ensures
		// returning an error causes a requeue without forcing a hotloop
		return err
	}

	return manageMachineSetsErr
}

// manageMachineSets checks and updates machine sets for the given cluster.
// Does NOT modify <machineSets>.
// It will requeue the cluster in case of an error while creating/deleting machine sets.
func (c *ClusterController) manageMachineSets(machineSets []*clusteroperator.MachineSet, cluster *clusteroperator.Cluster) error {
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for Cluster %#v: %v", cluster, err))
		return nil
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

		machineSetToCreate, deleteMachineSet, err := c.manageMachineSet(cluster, clusterMachineSets[i], machineSetConfig, machineSetPrefixes[i])
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
		glog.V(2).Infof("Creating %d new machine sets for cluster %q/%q", len(machineSetsToCreate), cluster.Namespace, cluster.Name)
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
					glog.V(2).Infof("Failed creation, decrementing expectations for cluster %q/%q", cluster.Namespace, cluster.Name)
					c.expectations.CreationObserved(clusterKey)
					errCh <- err
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
					glog.V(2).Infof("Failed to delete %v, decrementing expectations for controller %q/%q", machineSetKey, cluster.Namespace, cluster.Name)
					c.expectations.DeletionObserved(clusterKey, machineSetKey)
					errCh <- err
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

func (c *ClusterController) manageMachineSet(cluster *clusteroperator.Cluster, machineSet *clusteroperator.MachineSet, clusterMachineSetConfig clusteroperator.MachineSetConfig, machineSetNamePrefix string) (*clusteroperator.MachineSet, bool, error) {
	if machineSet == nil {
		machineSet, err := buildNewMachineSet(cluster, clusterMachineSetConfig, machineSetNamePrefix)
		return machineSet, false, err
	}

	if !apiequality.Semantic.DeepEqual(machineSet.Spec.MachineSetConfig, clusterMachineSetConfig) {
		glog.V(2).Infof("The configuration of the machine set %s has changed from %v to %v", machineSet.Name, machineSet.Spec.MachineSetConfig, clusterMachineSetConfig)
		machineSet, err := buildNewMachineSet(cluster, clusterMachineSetConfig, machineSetNamePrefix)
		return machineSet, true, err
	}

	return nil, false, nil
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (c *ClusterController) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *clusteroperator.Cluster {
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

func calculateStatus(cluster *clusteroperator.Cluster, machineSets []*clusteroperator.MachineSet, manageMachineSetsErr error) clusteroperator.ClusterStatus {
	newStatus := cluster.Status

	newStatus.MachineSetCount = 0
	for _, ms := range cluster.Spec.MachineSets {
		if machineSet := findMachineSetWithPrefix(machineSets, getNamePrefixForMachineSet(cluster, ms.Name)); machineSet != nil {
			newStatus.MachineSetCount++
			if machineSet.Spec.NodeType == clusteroperator.NodeTypeMaster {
				newStatus.MasterMachineSetName = machineSet.Name
			}
			if machineSet.Spec.Infra {
				newStatus.InfraMachineSetName = machineSet.Name
			}
		}
	}

	return newStatus
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

func buildNewMachineSet(cluster *clusteroperator.Cluster, machineSetConfig clusteroperator.MachineSetConfig, machineSetNamePrefix string) (*clusteroperator.MachineSet, error) {
	return &clusteroperator.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    machineSetNamePrefix,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cluster, controllerKind)},
		},
		Spec: clusteroperator.MachineSetSpec{
			MachineSetConfig: machineSetConfig,
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

func (c *ClusterController) updateClusterStatus(cluster *clusteroperator.Cluster, newStatus clusteroperator.ClusterStatus) (*clusteroperator.Cluster, error) {
	if cluster.Status.MachineSetCount == newStatus.MachineSetCount &&
		cluster.Status.MasterMachineSetName == newStatus.MasterMachineSetName &&
		cluster.Status.InfraMachineSetName == newStatus.InfraMachineSetName {
		return cluster, nil
	}

	cluster.Status = newStatus
	return c.client.ClusteroperatorV1alpha1().Clusters(cluster.Namespace).UpdateStatus(cluster)
}
