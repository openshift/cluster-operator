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
	"github.com/golang/glog"

	"github.com/staebler/boatswain/pkg/apis/boatswain/v1alpha1"
	"github.com/staebler/boatswain/pkg/pretty"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

func (c *controller) hostAdd(obj interface{}) {
	// DeletionHandlingMetaNamespaceKeyFunc returns a unique key for the resource and
	// handles the special case where the resource is of DeletedFinalStateUnknown type, which
	// acts a place holder for resources that have been deleted from storage but the watch event
	// confirming the deletion has not yet arrived.
	// Generally, the key is "namespace/name" for namespaced-scoped resources and
	// just "name" for cluster scoped resources.
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		glog.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	c.hostQueue.Add(key)
}

func (c *controller) hostUpdate(oldObj, newObj interface{}) {
	c.hostAdd(newObj)
}

func (c *controller) hostDelete(obj interface{}) {
	host, ok := obj.(*v1alpha1.Host)
	if host == nil || !ok {
		return
	}

	glog.V(4).Infof("Received delete event for Host %v; no further processing will occur", host.Name)
}

func (c *controller) reconcileHostKey(key string) error {
	host, err := c.hostLister.Get(key)
	pcb := pretty.NewContextBuilder(pretty.Host, "", key)
	if errors.IsNotFound(err) {
		glog.Info(pcb.Message("Not doing work because it has been deleted"))
		return nil
	}
	if err != nil {
		glog.Info(pcb.Messagef("Unable to retrieve object from store: %v", err))
		return err
	}

	return c.reconcileHost(host)
}

// reconcileHost is the control-loop that reconciles a Host. An
// error is returned to indicate that the host has not been fully
// processed and should be resubmitted at a later time.
func (c *controller) reconcileHost(host *v1alpha1.Host) error {
	pcb := pretty.NewContextBuilder(pretty.Host, "", host.Name)
	glog.V(4).Infof(pcb.Message("Processing"))

	return nil
}
