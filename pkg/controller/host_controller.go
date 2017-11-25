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

package controller

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
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

// NewHostController returns a new *HostController.
func NewHostController(hostInformer informers.HostInformer, kubeClient kubeclientset.Interface, boatswainClient boatswainclientset.Interface) *HostController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("boatswain_host_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &HostController{
		client: boatswainClient,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "host"),
	}

	hostInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addHost,
		UpdateFunc: c.updateHost,
		DeleteFunc: c.deleteHost,
	})
	c.hostsLister = hostInformer.Lister()
	c.hostsSynced = hostInformer.Informer().HasSynced

	c.syncHandler = c.syncHost
	c.enqueueHost = c.enqueue

	return c
}

// HostController manages provisioning hosts.
type HostController struct {
	client boatswainclientset.Interface

	// To allow injection of syncDeployment for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueHost func(host *boatswain.Host)

	// hostsLister is able to list/get hosts and is populated by the shared informer passed to
	// NewHostController.
	hostsLister lister.HostLister
	// hostsSynced returns true if the host shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	hostsSynced cache.InformerSynced

	// Hosts that need to be synced
	queue workqueue.RateLimitingInterface
}

func (c *HostController) addHost(obj interface{}) {
	h := obj.(*boatswain.Host)
	glog.V(4).Infof("Adding host %s", h.Name)
	c.enqueueHost(h)
}

func (c *HostController) updateHost(old, cur interface{}) {
	oldH := old.(*boatswain.Host)
	curH := cur.(*boatswain.Host)
	glog.V(4).Infof("Updating host %s", oldH.Name)
	c.enqueueHost(curH)
}

func (c *HostController) deleteHost(obj interface{}) {
	h, ok := obj.(*boatswain.Host)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		h, ok = tombstone.Obj.(*boatswain.Host)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Host %#v", obj))
			return
		}
	}
	glog.V(4).Infof("Deleting host %s", h.Name)
	c.enqueueHost(h)
}

// Runs c; will not return until stopCh is closed. workers determines how many
// hosts will be handled in parallel.
func (c *HostController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting host controller")
	defer glog.Infof("Shutting down host controller")

	if !WaitForCacheSync("host", stopCh, c.hostsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *HostController) enqueue(host *boatswain.Host) {
	key, err := KeyFunc(host)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", host, err))
		return
	}

	c.queue.Add(key)
}

func (c *HostController) enqueueRateLimited(host *boatswain.Host) {
	key, err := KeyFunc(host)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", host, err))
		return
	}

	c.queue.AddRateLimited(key)
}

// enqueueAfter will enqueue a host after the provided amount of time.
func (c *HostController) enqueueAfter(host *boatswain.Host, after time.Duration) {
	key, err := KeyFunc(host)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", host, err))
		return
	}

	c.queue.AddAfter(key, after)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *HostController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *HostController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *HostController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing host %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping host %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncHost will sync the host with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *HostController) syncHost(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing host %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing host %q (%v)", key, time.Since(startTime))
	}()

	host, err := c.hostsLister.Get(key)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("Host %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	h := host.DeepCopy()

	glog.V(4).Infof("Provisioning host %q", h.Name)

	return nil
}
