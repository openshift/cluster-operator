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

package node

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

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	informers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	lister "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

// NewNodeController returns a new *NodeController.
func NewNodeController(nodeInformer informers.NodeInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *NodeController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_node_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &NodeController{
		client: clusteroperatorClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "node"),
	}

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNode,
		UpdateFunc: c.updateNode,
		DeleteFunc: c.deleteNode,
	})
	c.nodesLister = nodeInformer.Lister()
	c.nodesSynced = nodeInformer.Informer().HasSynced

	c.syncHandler = c.syncNode
	c.enqueueNode = c.enqueue

	return c
}

// NodeController monitors nodes creating in NodeGroups.
type NodeController struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncNode for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueNode func(node *clusteroperator.Node)

	// nodesLister is able to list/get nodes and is populated by the shared informer passed to
	// NewNodeController.
	nodesLister lister.NodeLister
	// nodesSynced returns true if the node shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	nodesSynced cache.InformerSynced

	// Nodes that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *NodeController) addNode(obj interface{}) {
	h := obj.(*clusteroperator.Node)
	glog.V(4).Infof("Adding node %s", h.Name)
	c.enqueueNode(h)
}

func (c *NodeController) updateNode(old, cur interface{}) {
	oldH := old.(*clusteroperator.Node)
	curH := cur.(*clusteroperator.Node)
	glog.V(4).Infof("Updating node %s", oldH.Name)
	c.enqueueNode(curH)
}

func (c *NodeController) deleteNode(obj interface{}) {
	h, ok := obj.(*clusteroperator.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		h, ok = tombstone.Obj.(*clusteroperator.Node)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Node %#v", obj))
			return
		}
	}
	glog.V(4).Infof("Deleting node %s", h.Name)
	c.enqueueNode(h)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// nodes will be handled in parallel.
func (c *NodeController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting node controller")
	defer glog.Infof("Shutting down node controller")

	if !controller.WaitForCacheSync("node", stopCh, c.nodesSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *NodeController) enqueue(node *clusteroperator.Node) {
	key, err := controller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.Add(key)
}

func (c *NodeController) enqueueRateLimited(node *clusteroperator.Node) {
	key, err := controller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a node after the provided amount of time.
func (c *NodeController) enqueueAfter(node *clusteroperator.Node, after time.Duration) {
	key, err := controller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *NodeController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *NodeController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *NodeController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing node %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping node %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncNode will sync the node with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *NodeController) syncNode(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing node %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing node %q (%v)", key, time.Since(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid node key %q: either namespace or name is missing", key)
	}

	node, err := c.nodesLister.Nodes(ns).Get(name)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("Node %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	h := node.DeepCopy()

	glog.V(4).Infof("Reconciling node %q", h.Name)

	return nil
}
