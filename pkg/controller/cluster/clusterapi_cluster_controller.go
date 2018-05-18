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

package cluster

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"

	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	informers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	listers "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"

	"github.com/openshift/cluster-operator/pkg/controller"
	colog "github.com/openshift/cluster-operator/pkg/logging"
)

var clusterAPIControllerKind = clusterapi.SchemeGroupVersion.WithKind("Cluster")

const (
	clusterAPIControllerLogName = "clusterapi_cluster"
	clusterAPIClusterNameLabel  = "clusteroperator.openshift.io/cluster"
)

// NewClusterAPIController returns a new controller.
func NewClusterAPIController(clusterInformer informers.ClusterInformer, machineSetInformer informers.MachineSetInformer, kubeClient kubeclientset.Interface, clusterAPIClient clusterapiclient.Interface) *CAPIController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage("clusterapi_cluster_controller", kubeClient.CoreV1().RESTClient().GetRateLimiter())
	}

	logger := log.WithField("controller", clusterAPIControllerLogName)
	c := &CAPIController{
		client:     clusterAPIClient,
		kubeClient: kubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "clusterapi_cluster"),
		logger:     logger,
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
	})
	c.machineSetsLister = machineSetInformer.Lister()
	c.machineSetsSynced = machineSetInformer.Informer().HasSynced

	c.syncHandler = c.syncCluster
	c.enqueueCluster = c.enqueue

	return c
}

// CAPIController manages clusters.
type CAPIController struct {
	client     clusterapiclient.Interface
	kubeClient kubeclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error
	// used for unit testing
	enqueueCluster func(cluster *clusterapi.Cluster)

	// clustersLister is able to list/get clusters and is populated by the shared informer passed to
	// NewController.
	clustersLister listers.ClusterLister
	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// machineSetsLister is able to list/get machine sets and is populated by the shared informer passed to
	// NewController.
	machineSetsLister listers.MachineSetLister
	// machineSetsSynced returns true if the machine set shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	machineSetsSynced cache.InformerSynced

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	logger log.FieldLogger
}

func (c *CAPIController) addCluster(obj interface{}) {
	cluster := obj.(*clusterapi.Cluster)
	colog.WithClusterAPICluster(c.logger, cluster).Debugf("adding cluster")
	c.enqueueCluster(cluster)
}

func (c *CAPIController) updateCluster(old, cur interface{}) {
	oldCluster := old.(*clusterapi.Cluster)
	curCluster := cur.(*clusterapi.Cluster)
	colog.WithClusterAPICluster(c.logger, oldCluster).Debugf("updating cluster")
	c.enqueueCluster(curCluster)
}

func (c *CAPIController) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*clusterapi.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*clusterapi.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	colog.WithClusterAPICluster(c.logger, cluster).Debugf("deleting cluster")
	c.enqueueCluster(cluster)
}

// When a machine set is created, enqueue the cluster that
func (c *CAPIController) addMachineSet(obj interface{}) {
	machineSet := obj.(*clusterapi.MachineSet)

	if machineSet.DeletionTimestamp != nil {
		return
	}

	cluster, err := controller.ClusterForClusterAPIMachineSet(machineSet, c.clustersLister)
	if err != nil {
		glog.V(2).Infof("error retrieving cluster for machine set %q/%q: %v", machineSet.Namespace, machineSet.Name, err)
		return
	}
	if cluster == nil {
		// If the cluster owner reference is not set, attempt to retrieve the cluster by label
		if machineSet.Labels != nil {
			if clusterName, ok := machineSet.Labels[clusterAPIClusterNameLabel]; ok {
				cluster, err = c.clustersLister.Clusters(machineSet.Namespace).Get(clusterName)
				if err != nil {
					glog.V(2).Infof("error retrieving cluster for machineset %s/%s: %v", machineSet.Namespace, machineSet.Name, err)
					return
				}
			}
		}
		if cluster == nil {
			return
		}
	}
	colog.WithClusterAPIMachineSet(colog.WithClusterAPICluster(c.logger, cluster), machineSet).Debugln("machineset created")
	c.enqueueCluster(cluster)
}

// When a machine set is updated, figure out what cluster manages it and wake it
// up.
func (c *CAPIController) updateMachineSet(old, cur interface{}) {
	oldMachineSet := old.(*clusterapi.MachineSet)
	curMachineSet := cur.(*clusterapi.MachineSet)
	if curMachineSet.ResourceVersion == oldMachineSet.ResourceVersion {
		// Periodic resync will send update events for all known machine sets.
		// Two different versions of the same machine set will always have different RVs.
		return
	}

	if curMachineSet.DeletionTimestamp != nil {
		return
	}

	cluster, err := controller.ClusterForClusterAPIMachineSet(curMachineSet, c.clustersLister)
	if err != nil {
		glog.V(2).Infof("error retrieving cluster for machine set %s/%s: %v", curMachineSet.Namespace, curMachineSet.Name, err)
		return
	}
	if cluster == nil {
		// If the cluster owner reference is not set, attempt to retrieve the cluster by label
		if curMachineSet.Labels != nil {
			if clusterName, ok := curMachineSet.Labels[clusterAPIClusterNameLabel]; ok {
				cluster, err = c.clustersLister.Clusters(curMachineSet.Namespace).Get(clusterName)
				if err != nil {
					glog.V(2).Infof("error retrieving cluster for machineset %s/%s: %v", curMachineSet.Namespace, curMachineSet.Name, err)
					return
				}
			}
		}
		if cluster == nil {
			return
		}
	}
	colog.WithClusterAPIMachineSet(colog.WithClusterAPICluster(c.logger, cluster), curMachineSet).Debugln("machineset updated")
	c.enqueueCluster(cluster)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *CAPIController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Info("starting cluster controller")
	defer c.logger.Info("shutting down cluster controller")

	if !controller.WaitForCacheSync("clusterapi_cluster", stopCh, c.clustersSynced, c.machineSetsSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *CAPIController) enqueue(cluster *clusterapi.Cluster) {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *CAPIController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *CAPIController) processNextWorkItem() bool {
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
func (c *CAPIController) syncCluster(key string) error {
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
		return nil
	}
	if err != nil {
		return err
	}

	// List all active machine sets
	allMachineSets, err := c.machineSetsLister.MachineSets(cluster.Namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	r, err := labels.NewRequirement(clusterAPIClusterNameLabel, selection.Equals, []string{cluster.Name})
	if err != nil {
		return err
	}
	machineSetsSelector := labels.NewSelector().Add(*r)

	// If any adoptions are attempted, we should first recheck for deletion with
	// an uncached quorum read sometime after listing ReplicaSets (see #42639).
	canAdoptFunc := RecheckDeletionTimestamp(func() (metav1.Object, error) {
		fresh, err := c.client.ClusterV1alpha1().Clusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if fresh.UID != cluster.UID {
			return nil, fmt.Errorf("original Cluster %s/%s is gone: got uid %v, wanted %v", cluster.Namespace, cluster.Name, fresh.UID, cluster.UID)
		}
		return fresh, nil
	})
	refManager := NewMachineSetControllerRefManager(c.client.ClusterV1alpha1(), cluster, machineSetsSelector, clusterAPIControllerKind, canAdoptFunc)
	_, err = refManager.ClaimMachineSets(allMachineSets)
	return err
}
