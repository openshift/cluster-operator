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

package masternode

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

// NewMasterNodeController returns a new *MasterNodeController.
func NewMasterNodeController(nodeInformer informers.NodeInformer, kubeClient kubeclientset.Interface, clusteroperatorClient clusteroperatorclientset.Interface) *MasterNodeController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_master_node_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &MasterNodeController{
		client: clusteroperatorClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "masterNode"),
	}

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNode,
		UpdateFunc: c.updateNode,
		DeleteFunc: c.deleteNode,
	})
	c.nodesLister = nodeInformer.Lister()
	c.nodesSynced = nodeInformer.Informer().HasSynced

	c.syncHandler = c.syncMasterNode
	c.enqueueMasterNode = c.enqueue

	return c
}

// MasterNodeController manages launching the master/control plane on nodes
// that are masters in the cluster.
type MasterNodeController struct {
	client clusteroperatorclientset.Interface

	// To allow injection of syncNode for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueMasterNode func(masterNode *clusteroperator.Node)

	// nodesLister is able to list/get nodes and is populated by the shared informer passed to
	// NewMasterNodeController.
	nodesLister lister.NodeLister
	// nodesSynced returns true if the node shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	nodesSynced cache.InformerSynced

	// Nodes that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *MasterNodeController) addNode(obj interface{}) {
	n := obj.(*clusteroperator.Node)
	if !isMasterNode(n) {
		return
	}
	glog.V(4).Infof("Adding master node %s", n.Name)
	c.enqueueMasterNode(n)
}

func (c *MasterNodeController) updateNode(old, cur interface{}) {
	oldN := old.(*clusteroperator.Node)
	curN := cur.(*clusteroperator.Node)
	if !isMasterNode(curN) {
		return
	}
	glog.V(4).Infof("Updating master node %s", oldN.Name)
	c.enqueueMasterNode(curN)
}

func (c *MasterNodeController) deleteNode(obj interface{}) {
	n, ok := obj.(*clusteroperator.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		n, ok = tombstone.Obj.(*clusteroperator.Node)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Node %#v", obj))
			return
		}
	}
	if !isMasterNode(n) {
		return
	}
	glog.V(4).Infof("Deleting master node %s", n.Name)
	c.enqueueMasterNode(n)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// nodes will be handled in parallel.
func (c *MasterNodeController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting master node controller")
	defer glog.Infof("Shutting down master node controller")

	if !controller.WaitForCacheSync("node", stopCh, c.nodesSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *MasterNodeController) enqueue(node *clusteroperator.Node) {
	key, err := controller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.Add(key)
}

func (c *MasterNodeController) enqueueRateLimited(node *clusteroperator.Node) {
	key, err := controller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a node after the provided amount of time.
func (c *MasterNodeController) enqueueAfter(node *clusteroperator.Node, after time.Duration) {
	key, err := controller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *MasterNodeController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *MasterNodeController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *MasterNodeController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing master node %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping master node %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncMasterNode will sync the msater node with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *MasterNodeController) syncMasterNode(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing master node %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing master node %q (%v)", key, time.Since(startTime))
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
		glog.V(2).Infof("Master node %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	if !isMasterNode(node) {
		return nil
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	n := node.DeepCopy()

	glog.V(4).Infof("Reconciling master node %q", n.Name)

	return nil
}

func isMasterNode(node *clusteroperator.Node) bool {
	return node.Spec.NodeType == clusteroperator.NodeTypeMaster
}
