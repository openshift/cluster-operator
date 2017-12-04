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

package nodegroup

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"

	"github.com/staebler/boatswain/pkg/kubernetes/pkg/util/metrics"

	boatswain "github.com/staebler/boatswain/pkg/apis/boatswain/v1alpha1"
	boatswainclientset "github.com/staebler/boatswain/pkg/client/clientset_generated/clientset"
	informers "github.com/staebler/boatswain/pkg/client/informers_generated/externalversions/boatswain/v1alpha1"
	lister "github.com/staebler/boatswain/pkg/client/listers_generated/boatswain/v1alpha1"
	"github.com/staebler/boatswain/pkg/controller"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

// NewNodeGroupController returns a new *NodeGroupController.
func NewNodeGroupController(nodeGroupInformer informers.NodeGroupInformer, kubeClient kubeclientset.Interface, boatswainClient boatswainclientset.Interface) *NodeGroupController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("boatswain_node_group_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &NodeGroupController{
		client: boatswainClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodeGroup"),
	}

	nodeGroupInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNodeGroup,
		UpdateFunc: c.updateNodeGroup,
		DeleteFunc: c.deleteNodeGroup,
	})
	c.nodeGroupsLister = nodeGroupInformer.Lister()
	c.nodeGroupsSynced = nodeGroupInformer.Informer().HasSynced

	c.syncHandler = c.syncNodeGroup
	c.enqueueNodeGroup = c.enqueue

	return c
}

// NodeGroupController manages provisioning node groups.
type NodeGroupController struct {
	client boatswainclientset.Interface

	// To allow injection of syncNodeGroup for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueNodeGroup func(nodeGroup *boatswain.NodeGroup)

	// nodeGroupsLister is able to list/get node groups and is populated by the shared informer passed to
	// NewNodeGroupController.
	nodeGroupsLister lister.NodeGroupLister
	// nodeGroupsSynced returns true if the node group shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	nodeGroupsSynced cache.InformerSynced

	// NodeGroups that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *NodeGroupController) addNodeGroup(obj interface{}) {
	h := obj.(*boatswain.NodeGroup)
	glog.V(4).Infof("Adding node group %s", h.Name)
	c.enqueueNodeGroup(h)
}

func (c *NodeGroupController) updateNodeGroup(old, cur interface{}) {
	oldNg := old.(*boatswain.NodeGroup)
	curNg := cur.(*boatswain.NodeGroup)
	glog.V(4).Infof("Updating node group %s", oldNg.Name)
	c.enqueueNodeGroup(curNg)
}

func (c *NodeGroupController) deleteNodeGroup(obj interface{}) {
	ng, ok := obj.(*boatswain.NodeGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		ng, ok = tombstone.Obj.(*boatswain.NodeGroup)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a NodeGroup %#v", obj))
			return
		}
	}
	glog.V(4).Infof("Deleting node group %s", ng.Name)
	c.enqueueNodeGroup(ng)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// node groups will be handled in parallel.
func (c *NodeGroupController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting node group controller")
	defer glog.Infof("Shutting down node group controller")

	if !controller.WaitForCacheSync("nodegroup", stopCh, c.nodeGroupsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *NodeGroupController) enqueue(nodeGroup *boatswain.NodeGroup) {
	key, err := controller.KeyFunc(nodeGroup)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", nodeGroup, err))
		return
	}

	c.queue.Add(key)
}

func (c *NodeGroupController) enqueueRateLimited(nodeGroup *boatswain.NodeGroup) {
	key, err := controller.KeyFunc(nodeGroup)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", nodeGroup, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a node group after the provided amount of time.
func (c *NodeGroupController) enqueueAfter(nodeGroup *boatswain.NodeGroup, after time.Duration) {
	key, err := controller.KeyFunc(nodeGroup)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", nodeGroup, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *NodeGroupController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *NodeGroupController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *NodeGroupController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing node group %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping node group %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncNodeGroup will sync the node group with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *NodeGroupController) syncNodeGroup(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing node group %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing node group %q (%v)", key, time.Since(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid nodegroup key %q: either namespace or name is missing", key)
	}

	nodeGroup, err := c.nodeGroupsLister.NodeGroups(ns).Get(name)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("Node group %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	ng := nodeGroup.DeepCopy()

	glog.V(4).Infof("Provisioning node group %q", ng.Name)

	return nil
}
