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
	"github.com/staebler/boatswain/pkg/api"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	"github.com/staebler/boatswain/pkg/apis/boatswain"
	"github.com/staebler/boatswain/pkg/apis/boatswain/validation"
)

// NewScopeStrategy returns a new NamespaceScopedStrategy for nodegroups
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return nodegroupRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type nodegroupRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type nodegroupStatusRESTStrategy struct {
	nodegroupRESTStrategy
}

var (
	nodegroupRESTStrategies = nodegroupRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = nodegroupRESTStrategies
	_ rest.RESTUpdateStrategy = nodegroupRESTStrategies
	_ rest.RESTDeleteStrategy = nodegroupRESTStrategies

	nodegroupStatusUpdateStrategy = nodegroupStatusRESTStrategy{
		nodegroupRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = nodegroupStatusUpdateStrategy
)

// Canonicalize does not transform a nodegroup.
func (nodegroupRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to create")
	}
}

// NamespaceScoped returns true as nodegroups are scoped to a namespace.
func (nodegroupRESTStrategy) NamespaceScoped() bool {
	return true
}

// PrepareForCreate receives a the incoming NodeGroup and clears it's
// Status. Status is not a user settable field.
func (nodegroupRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	nodegroup, ok := obj.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	nodegroup.Status = boatswain.NodeGroupStatus{}
}

func (nodegroupRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateNodeGroup(obj.(*boatswain.NodeGroup))
}

func (nodegroupRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (nodegroupRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (nodegroupRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newNodeGroup, ok := new.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to update to")
	}
	oldNodeGroup, ok := old.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to update from")
	}

	newNodeGroup.Status = oldNodeGroup.Status
}

func (nodegroupRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newNodeGroup, ok := new.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to validate to")
	}
	oldNodeGroup, ok := old.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to validate from")
	}

	return validation.ValidateNodeGroupUpdate(newNodeGroup, oldNodeGroup)
}

func (nodegroupStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newNodeGroup, ok := new.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to update to")
	}
	oldNodeGroup, ok := old.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to update from")
	}
	// status changes are not allowed to update spec
	newNodeGroup.Spec = oldNodeGroup.Spec
}

func (nodegroupStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newNodeGroup, ok := new.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to validate to")
	}
	oldNodeGroup, ok := old.(*boatswain.NodeGroup)
	if !ok {
		glog.Fatal("received a non-nodegroup object to validate from")
	}

	return validation.ValidateNodeGroupStatusUpdate(newNodeGroup, oldNodeGroup)
}
