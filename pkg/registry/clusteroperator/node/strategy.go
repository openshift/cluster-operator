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
	"github.com/openshift/cluster-operator/pkg/api"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/validation"
)

// NewScopeStrategy returns a new NamespaceScopedStrategy for nodes
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return nodeRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type nodeRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type nodeStatusRESTStrategy struct {
	nodeRESTStrategy
}

var (
	nodeRESTStrategies = nodeRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = nodeRESTStrategies
	_ rest.RESTUpdateStrategy = nodeRESTStrategies
	_ rest.RESTDeleteStrategy = nodeRESTStrategies

	nodeStatusUpdateStrategy = nodeStatusRESTStrategy{
		nodeRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = nodeStatusUpdateStrategy
)

// Canonicalize does not transform a node.
func (nodeRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to create")
	}
}

// NamespaceScoped returns true as nodes are scoped to a namespace.
func (nodeRESTStrategy) NamespaceScoped() bool {
	return true
}

// PrepareForCreate receives a the incoming Node and clears it's
// Status. Status is not a user settable field.
func (nodeRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	node, ok := obj.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	node.Status = clusteroperator.NodeStatus{}
}

func (nodeRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateNode(obj.(*clusteroperator.Node))
}

func (nodeRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (nodeRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (nodeRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newNode, ok := new.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to update to")
	}
	oldNode, ok := old.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to update from")
	}

	newNode.Status = oldNode.Status
}

func (nodeRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newNode, ok := new.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to validate to")
	}
	oldNode, ok := old.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to validate from")
	}

	return validation.ValidateNodeUpdate(newNode, oldNode)
}

func (nodeStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newNode, ok := new.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to update to")
	}
	oldNode, ok := old.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to update from")
	}
	// status changes are not allowed to update spec
	newNode.Spec = oldNode.Spec
}

func (nodeStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newNode, ok := new.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to validate to")
	}
	oldNode, ok := old.(*clusteroperator.Node)
	if !ok {
		glog.Fatal("received a non-node object to validate from")
	}

	return validation.ValidateNodeStatusUpdate(newNode, oldNode)
}
