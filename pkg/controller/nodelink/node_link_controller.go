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

package nodelink

import (
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	lister "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"

	coclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	cometrics "github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries     = 15
	controllerName = "nodelink"

	// machineAnnotationKey is the annotation storing a link between a node and it's machine. Should match upstream cluster-api machine controller. (node.go)
	machineAnnotationKey = "machine"
	kubeClusterNamespace = "kube-cluster"
)

// NewController returns a new *Controller.
func NewController(
	nodeInformer coreinformers.NodeInformer,
	machineInformer capiinformers.MachineInformer,
	kubeClient kubeclientset.Interface,
	capiClient capiclient.Interface) *Controller {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		cometrics.RegisterMetricAndTrackRateLimiterUsage("clusteroperator_awselb_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	c := &Controller{
		capiClient: capiClient,
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodelink"),
	}

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNode,
		UpdateFunc: c.updateNode,
		DeleteFunc: c.deleteNode,
	})
	c.nodeLister = nodeInformer.Lister()
	c.nodesSynced = nodeInformer.Informer().HasSynced

	c.machinesLister = machineInformer.Lister()
	c.machinesSynced = machineInformer.Informer().HasSynced

	c.syncHandler = c.syncNode
	c.enqueueNode = c.enqueue
	c.logger = log.WithField("controller", controllerName)

	return c
}

// Controller monitors nodes and links them to their machines when possible, as well as applies desired labels and taints.
type Controller struct {
	client     coclientset.Interface
	capiClient capiclient.Interface
	kubeClient kubeclientset.Interface

	// To allow injection for testing.
	syncHandler func(hKey string) error

	// used for unit testing
	enqueueNode func(node *corev1.Node)

	nodeLister  corelister.NodeLister
	nodesSynced cache.InformerSynced

	machinesLister lister.MachineLister
	machinesSynced cache.InformerSynced

	// Machines that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *Controller) addNode(obj interface{}) {
	node := obj.(*corev1.Node)
	c.logger.WithField("node", node.Name).Debug("adding node")
	c.enqueueNode(node)
}

func (c *Controller) updateNode(old, cur interface{}) {
	//oldNode := old.(*corev1.Node)
	curNode := cur.(*corev1.Node)

	c.logger.WithField("node", curNode.Name).Debug("updating node")
	c.enqueueNode(curNode)
}

func (c *Controller) deleteNode(obj interface{}) {

	node, ok := obj.(*corev1.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		node, ok = tombstone.Obj.(*corev1.Node)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Node %#v", obj))
			return
		}
	}

	c.logger.WithField("node", node.Name).Debug("deleting node")
	c.enqueueNode(node)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many machines will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Infof("Starting nodelink controller")
	defer log.Infof("Shutting down nodelink controller")

	if !cocontroller.WaitForCacheSync("machine", stopCh, c.machinesSynced) {
		return
	}

	if !cocontroller.WaitForCacheSync("node", stopCh, c.nodesSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(node *corev1.Node) {
	key, err := cocontroller.KeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", node, err))
		return
	}

	c.queue.Add(key)
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
	c.handleErr(err, key)

	return true
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		c.logger.Infof("Error syncing node %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	c.logger.Infof("Dropping node %q out of the queue: %v", key, err)
	c.queue.Forget(key)
}

// syncNode will sync the node with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (c *Controller) syncNode(key string) error {
	startTime := time.Now()
	c.logger.WithField("key", key).Debug("syncing node")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing node")
	}()

	_, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	node, err := c.nodeLister.Get(key)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Info("node has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	return c.processNode(node)
}
func (c *Controller) processNode(node *corev1.Node) error {
	nodeLog := c.logger.WithField("node", node.Name)
	machineKey, ok := node.Annotations[machineAnnotationKey]
	// No machine annotation, this is likely the first time we've seen the node,
	// need to load all machines and search for one with matching IP.
	var matchingMachine *capiv1.Machine
	if ok {
		var err error
		namespace, machineName, err := cache.SplitMetaNamespaceKey(machineKey)
		if err != nil {
			nodeLog.Infof("machine annotation format is incorrect %v: %v\n", machineKey, err)
			return err
		}
		matchingMachine, err = c.machinesLister.Machines(namespace).Get(machineName)
		// If machine matching annotation is not found, we'll still try to find one via IP matching:
		if err != nil {
			if errors.IsNotFound(err) {
				nodeLog.WithField("machine", machineKey).Warn("machine matching node has been deleted, will attempt to find new machine by IP")
			} else {
				return err
			}
		}
	}

	if matchingMachine == nil {
		// Find this nodes internal IP so we can search for a matching machine:
		var nodeInternalIP string
		for _, a := range node.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				nodeInternalIP = a.Address
				break
			}
		}
		if nodeInternalIP == "" {
			nodeLog.Warnf("unable to find InternalIP for node")
			return fmt.Errorf("unable to find InternalIP for node: %s", node.Name)
		}

		allMachines, err := c.machinesLister.Machines(kubeClusterNamespace).List(labels.Everything())
		if err != nil {
			return err
		}
		nodeLog.Debugf("searching %d machines for IP match for node", len(allMachines))
		for _, m := range allMachines {
			for _, a := range m.Status.Addresses {
				// Use the internal IP to look for matches:
				if a.Type == corev1.NodeInternalIP && a.Address == nodeInternalIP {
					matchingMachine = m
					nodeLog.Infof("found matching machine: %s", matchingMachine.Name)
					break
				}
			}
		}
	}

	if matchingMachine == nil {
		nodeLog.Warnf("no matching machine found for node")
		return fmt.Errorf("no matching machine found for node: %s", node.Name)
	}

	modNode := node.DeepCopy()

	modNode.Annotations["machine"] = fmt.Sprintf("%s/%s", matchingMachine.Namespace, matchingMachine.Name)

	if modNode.Labels == nil {
		modNode.Labels = map[string]string{}
	}
	for k, v := range matchingMachine.Spec.Labels {
		nodeLog.Debugf("copying label %s = %s", k, v)
		modNode.Labels[k] = v
	}

	// Taints are to be an authoritative list on the machine spec per cluster-api comments:
	modNode.Spec.Taints = matchingMachine.Spec.Taints

	if !reflect.DeepEqual(node, modNode) {
		nodeLog.Debug("node has changed, updating")
		_, err := c.kubeClient.CoreV1().Nodes().Update(modNode)
		if err != nil {
			nodeLog.Errorf("error updating node: %v", err)
			return err
		}
	}
	return nil
}
