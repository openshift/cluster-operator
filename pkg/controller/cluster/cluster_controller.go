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

// NewClusterController returns a new *ClusterController.
func NewClusterController(clusterInformer informers.ClusterInformer, kubeClient kubeclientset.Interface, boatswainClient boatswainclientset.Interface) *ClusterController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("boatswain_cluster_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &ClusterController{
		client: boatswainClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})
	c.clustersLister = clusterInformer.Lister()
	c.clustersSynced = clusterInformer.Informer().HasSynced

	c.syncHandler = c.syncCluster
	c.enqueueCluster = c.enqueue

	return c
}

// ClusterController manages clusters.
type ClusterController struct {
	client boatswainclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueCluster func(cluster *boatswain.Cluster)

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewClusterController.
	clustersLister lister.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *ClusterController) addCluster(obj interface{}) {
	cluster := obj.(*boatswain.Cluster)
	glog.V(4).Infof("Adding cluster %s", cluster.Name)
	c.enqueueCluster(cluster)
}

func (c *ClusterController) updateCluster(old, cur interface{}) {
	oldCluster := old.(*boatswain.Cluster)
	curCluster := cur.(*boatswain.Cluster)
	glog.V(4).Infof("Updating cluster %s", oldCluster.Name)
	c.enqueueCluster(curCluster)
}

func (c *ClusterController) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*boatswain.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*boatswain.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	glog.V(4).Infof("Deleting cluster %s", cluster.Name)
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

func (c *ClusterController) enqueue(cluster *boatswain.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

func (c *ClusterController) enqueueRateLimited(cluster *boatswain.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a cluster after the provided amount of time.
func (c *ClusterController) enqueueAfter(cluster *boatswain.Cluster, after time.Duration) {
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
	c.handleErr(err, key)

	return true
}

func (c *ClusterController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing cluster %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping cluster %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncCluster will sync the cluster with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *ClusterController) syncCluster(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing cluster %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing cluster %q (%v)", key, time.Since(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid cluster key %q: either namespace or name is missing", key)
	}

	cluster, err := c.clustersLister.Clusters(ns).Get(name)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("Cluster %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	cluster = cluster.DeepCopy()

	glog.V(4).Infof("Provisioning cluster %q", cluster.Name)

	return nil
}
