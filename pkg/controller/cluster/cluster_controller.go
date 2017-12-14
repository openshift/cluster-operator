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
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
func NewClusterController(clusterInformer informers.ClusterInformer, nodeGroupInformer informers.NodeGroupInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *ClusterController {
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

	nodeGroupInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNodeGroup,
		UpdateFunc: c.updateNodeGroup,
		DeleteFunc: c.deleteNodeGroup,
	})
	c.nodeGroupsLister = nodeGroupInformer.Lister()
	c.nodeGroupsSynced = nodeGroupInformer.Informer().HasSynced

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

	// A TTLCache of node group creates/deletes each cluster expects to see.
	expectations *controller.UIDTrackingControllerExpectations

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewClusterController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// nodeGroupsLister is able to list/get node groups and is populated by the shared informer passed to
	// NewClusterController.
	nodeGroupsLister lister.NodeGroupLister
	// nodeGroupsSynced returns true if the node group shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	nodeGroupsSynced cache.InformerSynced

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

// When a node group is created, enqueue the cluster that manages it and update its expectations.
func (c *ClusterController) addNodeGroup(obj interface{}) {
	nodeGroup := obj.(*clusteroperator.NodeGroup)

	if nodeGroup.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a new node group shows up in a state that
		// is already pending deletion. Prevent the node group from being a creation observation.
		c.deleteNodeGroup(nodeGroup)
		return
	}

	controllerRef := metav1.GetControllerOf(nodeGroup)
	if controllerRef == nil {
		return
	}
	cluster := c.resolveControllerRef(nodeGroup.Namespace, controllerRef)
	if cluster == nil {
		return
	}
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		return
	}
	glog.V(4).Infof("Node group %s created: %#v.", nodeGroup.Name, nodeGroup)
	c.expectations.CreationObserved(clusterKey)
	c.enqueueCluster(cluster)
}

// When a node group is updated, figure out what cluster manages it and wake it
// up.
func (c *ClusterController) updateNodeGroup(old, cur interface{}) {
	oldNodeGroup := old.(*clusteroperator.NodeGroup)
	curNodeGroup := cur.(*clusteroperator.NodeGroup)
	if curNodeGroup.ResourceVersion == oldNodeGroup.ResourceVersion {
		// Periodic resync will send update events for all known node groups.
		// Two different versions of the same node group will always have different RVs.
		return
	}

	if curNodeGroup.DeletionTimestamp != nil {
		// when a node group is deleted gracefully it's deletion timestamp is first modified to reflect a grace period,
		// and after such time has passed, the kubelet actually deletes it from the store. We receive an update
		// for modification of the deletion timestamp and expect a cluster to create a replacement node group asap, not wait
		// until the kubelet actually deletes the node group. This is different from the Phase of a node group changing, because
		// a cluster never initiates a phase change, and so is never asleep waiting for the same.
		c.deleteNodeGroup(curNodeGroup)
		return
	}

	controllerRef := metav1.GetControllerOf(curNodeGroup)
	if controllerRef == nil {
		return
	}
	cluster := c.resolveControllerRef(curNodeGroup.Namespace, controllerRef)
	if cluster == nil {
		return
	}
	glog.V(4).Infof("Node group %s updated, objectMeta %+v -> %+v.", curNodeGroup.Name, oldNodeGroup.ObjectMeta, curNodeGroup.ObjectMeta)
	c.enqueueCluster(cluster)
}

// When a node group is deleted, enqueue the cluster that manages the node group and update its expectations.
func (c *ClusterController) deleteNodeGroup(obj interface{}) {
	nodeGroup, ok := obj.(*clusteroperator.NodeGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		nodeGroup, ok = tombstone.Obj.(*clusteroperator.NodeGroup)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}

	controllerRef := metav1.GetControllerOf(nodeGroup)
	if controllerRef == nil {
		return
	}
	cluster := c.resolveControllerRef(nodeGroup.Namespace, controllerRef)
	if cluster == nil {
		return
	}
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		return
	}
	glog.V(4).Infof("Node group %s/%s deleted through %v, timestamp %+v: %#v.", nodeGroup.Namespace, nodeGroup.Name, utilruntime.GetCaller(), nodeGroup.DeletionTimestamp, nodeGroup)
	c.expectations.DeletionObserved(clusterKey, getNodeGroupKey(nodeGroup))
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

	// List all active node groups controller by the cluster
	allNodeGroups, err := c.nodeGroupsLister.NodeGroups(cluster.Namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	var filteredNodeGroups []*clusteroperator.NodeGroup
	for _, nodeGroup := range allNodeGroups {
		if nodeGroup.DeletionTimestamp != nil {
			continue
		}
		controllerRef := metav1.GetControllerOf(nodeGroup)
		if controllerRef == nil {
			continue
		}
		if cluster.UID != controllerRef.UID {
			continue
		}
		filteredNodeGroups = append(filteredNodeGroups, nodeGroup)
	}

	var manageNodeGroupsErr error
	if clusterNeedsSync && cluster.DeletionTimestamp == nil {
		manageNodeGroupsErr = c.manageNodeGroups(filteredNodeGroups, cluster)
	}

	cluster = cluster.DeepCopy()

	newStatus := calculateStatus(cluster, filteredNodeGroups, manageNodeGroupsErr)

	// Always updates status as node groups come up or die.
	if _, err := c.updateClusterStatus(cluster, newStatus); err != nil {
		// Multiple things could lead to this update failing. Requeuing the cluster ensures
		// returning an error causes a requeue without forcing a hotloop
		return err
	}

	return manageNodeGroupsErr
}

// manageNodeGroups checks and updates node groups for the given cluster.
// Does NOT modify <nodeGroups>.
// It will requeue the cluster in case of an error while creating/deleting node groups.
func (c *ClusterController) manageNodeGroups(nodeGroups []*clusteroperator.NodeGroup, cluster *clusteroperator.Cluster) error {
	clusterKey, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for Cluster %#v: %v", cluster, err))
		return nil
	}

	var errCh chan error

	masterNodeGroupPrefix := getNamePrefixForMasterNodeGroup(cluster)
	computeNodeGroupsPrefixes := make([]string, len(cluster.Spec.ComputeNodeGroups))
	for i, ng := range cluster.Spec.ComputeNodeGroups {
		computeNodeGroupsPrefixes[i] = getNamePrefixForComputeNodeGroup(cluster, ng.Name)
	}

	var masterNodeGroup *clusteroperator.NodeGroup
	computeNodeGroups := make([]*clusteroperator.NodeGroup, len(cluster.Spec.ComputeNodeGroups))
	nodeGroupsToCreate := []*clusteroperator.NodeGroup{}
	nodeGroupsToDelete := []*clusteroperator.NodeGroup{}

	// Organize node groups
	for _, nodeGroup := range nodeGroups {
		switch nodeGroup.Spec.NodeType {
		case clusteroperator.NodeTypeMaster:
			if masterNodeGroup == nil {
				masterNodeGroup = nodeGroup
			} else {
				utilruntime.HandleError(fmt.Errorf("Found two active master node groups for cluster %s/%s: %s and %s", cluster.Namespace, cluster.Name, masterNodeGroup.Name, nodeGroup.Name))
				nodeGroupsToDelete = append(nodeGroupsToDelete, nodeGroup)
			}
		case clusteroperator.NodeTypeCompute:
			found := false
			for i, prefix := range computeNodeGroupsPrefixes {
				if strings.HasPrefix(nodeGroup.Name, prefix) {
					if computeNodeGroups[i] == nil {
						computeNodeGroups[i] = nodeGroup
					} else {
						utilruntime.HandleError(fmt.Errorf("Found two active conflicting compute node groups for cluster %s/%s: %s and %s", cluster.Namespace, cluster.Name, computeNodeGroups[i].Name, nodeGroup.Name))
						nodeGroupsToDelete = append(nodeGroupsToDelete, nodeGroup)
					}
					found = true
					break
				}
			}
			if !found {
				nodeGroupsToDelete = append(nodeGroupsToDelete, nodeGroup)
			}
		}
	}

	// Sync master node group
	masterNodeGroupToCreate, deleteMasterNodeGroup, err := c.manageNodeGroup(cluster, masterNodeGroup, cluster.Spec.MasterNodeGroup, clusteroperator.NodeTypeMaster, masterNodeGroupPrefix)
	if err != nil {
		errCh <- err
	}
	if masterNodeGroupToCreate != nil {
		nodeGroupsToCreate = append(nodeGroupsToCreate, masterNodeGroupToCreate)
	}
	if deleteMasterNodeGroup {
		nodeGroupsToDelete = append(nodeGroupsToDelete, masterNodeGroup)
	}
	// Sync compute node groups
	for i := range cluster.Spec.ComputeNodeGroups {
		nodeGroupToCreate, deleteNodeGroup, err := c.manageNodeGroup(cluster, computeNodeGroups[i], cluster.Spec.ComputeNodeGroups[i].ClusterNodeGroup, clusteroperator.NodeTypeCompute, computeNodeGroupsPrefixes[i])
		if err != nil {
			errCh <- err
		}
		if nodeGroupToCreate != nil {
			nodeGroupsToCreate = append(nodeGroupsToCreate, nodeGroupToCreate)
		}
		if deleteNodeGroup {
			nodeGroupsToDelete = append(nodeGroupsToDelete, computeNodeGroups[i])
		}
	}

	// Snapshot the UIDs (ns/name) of the node groups we're expecting to see
	// deleted, so we know to record their expectations exactly once either
	// when we see it as an update of the deletion timestamp, or as a delete.
	deletedNodeGroupKeys := make([]string, len(nodeGroupsToDelete))
	for i, ng := range nodeGroupsToDelete {
		deletedNodeGroupKeys[i] = getNodeGroupKey(ng)
	}
	c.expectations.SetExpectations(clusterKey, len(nodeGroupsToCreate), deletedNodeGroupKeys)

	if len(nodeGroupsToCreate) > 0 {
		var wg sync.WaitGroup
		glog.V(2).Infof("Creating %d new node groups for cluster %q/%q", len(nodeGroupsToCreate), cluster.Namespace, cluster.Name)
		wg.Add(len(nodeGroupsToCreate))
		for i, ng := range nodeGroupsToCreate {
			go func(ix int, nodeGroup *clusteroperator.NodeGroup) {
				defer wg.Done()
				_, err := c.client.ClusteroperatorV1alpha1().NodeGroups(cluster.Namespace).Create(nodeGroup)
				if err != nil && errors.IsTimeout(err) {
					// Node group is created but its initialization has timed out.
					// If the initialization is successful eventually, the
					// controller will observe the creation via the informer.
					// If the initialization fails, or if the node group stays
					// uninitialized for a long time, the informer will not
					// receive any update, and the controller will create a new
					// node group when the expectation expires.
					return
				}
				if err != nil {
					// Decrement the expected number of creates because the informer won't observe this node group
					glog.V(2).Infof("Failed creation, decrementing expectations for cluster %q/%q", cluster.Namespace, cluster.Name)
					c.expectations.CreationObserved(clusterKey)
					errCh <- err
				}
			}(i, ng)
		}
		wg.Wait()
	}

	if len(nodeGroupsToDelete) > 0 {
		var wg sync.WaitGroup
		wg.Add(len(nodeGroupsToDelete))
		for i, ng := range nodeGroupsToDelete {
			go func(ix int, nodeGroup *clusteroperator.NodeGroup) {
				defer wg.Done()
				if err := c.client.ClusteroperatorV1alpha1().NodeGroups(cluster.Namespace).Delete(nodeGroup.Name, &metav1.DeleteOptions{}); err != nil {
					// Decrement the expected number of deletes because the informer won't observe this deletion
					nodeGroupKey := deletedNodeGroupKeys[ix]
					glog.V(2).Infof("Failed to delete %v, decrementing expectations for controller %q/%q", nodeGroupKey, cluster.Namespace, cluster.Name)
					c.expectations.DeletionObserved(clusterKey, nodeGroupKey)
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

func (c *ClusterController) manageNodeGroup(cluster *clusteroperator.Cluster, nodeGroup *clusteroperator.NodeGroup, clusterNodeGroup clusteroperator.ClusterNodeGroup, nodeType clusteroperator.NodeType, nodeGroupNamePrefix string) (*clusteroperator.NodeGroup, bool, error) {
	if nodeGroup == nil {
		return buildNewNodeGroup(cluster, clusterNodeGroup, nodeType, nodeGroupNamePrefix), false, nil
	}

	if nodeGroup.Spec.Size != clusterNodeGroup.Size {
		glog.V(2).Infof("Changing size of node group %s from %v to %v", nodeGroup.Name, nodeGroup.Spec.Size, clusterNodeGroup.Size)
		return buildNewNodeGroup(cluster, clusterNodeGroup, nodeType, nodeGroupNamePrefix), true, nil
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

func calculateStatus(cluster *clusteroperator.Cluster, nodeGroups []*clusteroperator.NodeGroup, manageNodeGroupsErr error) clusteroperator.ClusterStatus {
	newStatus := cluster.Status

	newStatus.MasterNodeGroups = 0
	if includesNodeGroupWithPrefix(nodeGroups, getNamePrefixForMasterNodeGroup(cluster)) {
		newStatus.MasterNodeGroups++
	}

	newStatus.ComputeNodeGroups = 0
	for _, ng := range cluster.Spec.ComputeNodeGroups {
		if includesNodeGroupWithPrefix(nodeGroups, getNamePrefixForComputeNodeGroup(cluster, ng.Name)) {
			newStatus.ComputeNodeGroups++
		}
	}

	return newStatus
}

func buildNewNodeGroup(cluster *clusteroperator.Cluster, clusterNodeGroup clusteroperator.ClusterNodeGroup, nodeType clusteroperator.NodeType, nodeGroupNamePrefix string) *clusteroperator.NodeGroup {
	boolPtr := func(b bool) *bool { return &b }
	return &clusteroperator.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: nodeGroupNamePrefix,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         controllerKind.GroupVersion().String(),
					Kind:               controllerKind.Kind,
					Name:               cluster.Name,
					UID:                cluster.UID,
					BlockOwnerDeletion: boolPtr(true),
					Controller:         boolPtr(true),
				},
			},
		},
		Spec: clusteroperator.NodeGroupSpec{
			NodeType: nodeType,
			Size:     clusterNodeGroup.Size,
		},
	}
}

func getNamePrefixForMasterNodeGroup(cluster *clusteroperator.Cluster) string {
	return fmt.Sprintf("%s-master-", cluster.Name)
}

func getNamePrefixForComputeNodeGroup(cluster *clusteroperator.Cluster, nodeGroupName string) string {
	return fmt.Sprintf("%s-compute-%s-", cluster.Name, nodeGroupName)
}

func includesNodeGroupWithPrefix(nodeGroups []*clusteroperator.NodeGroup, prefix string) bool {
	for _, nodeGroup := range nodeGroups {
		if strings.HasPrefix(nodeGroup.Name, prefix) {
			return true
		}
	}
	return false
}

func getNodeGroupKey(nodeGroup *clusteroperator.NodeGroup) string {
	return fmt.Sprintf("%s/%s", nodeGroup.Namespace, nodeGroup.Name)
}

func (c *ClusterController) updateClusterStatus(cluster *clusteroperator.Cluster, newStatus clusteroperator.ClusterStatus) (*clusteroperator.Cluster, error) {
	if cluster.Status.MasterNodeGroups == newStatus.MasterNodeGroups &&
		cluster.Status.ComputeNodeGroups == newStatus.ComputeNodeGroups {
		return cluster, nil
	}

	cluster.Status = newStatus
	return c.client.ClusteroperatorV1alpha1().Clusters(cluster.Namespace).UpdateStatus(cluster)
}
