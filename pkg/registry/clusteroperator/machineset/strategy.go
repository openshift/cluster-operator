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

package machineset

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

// NewScopeStrategy returns a new NamespaceScopedStrategy for machinesets
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return machinesetRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type machinesetRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type machinesetStatusRESTStrategy struct {
	machinesetRESTStrategy
}

var (
	machinesetRESTStrategies = machinesetRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = machinesetRESTStrategies
	_ rest.RESTUpdateStrategy = machinesetRESTStrategies
	_ rest.RESTDeleteStrategy = machinesetRESTStrategies

	machinesetStatusUpdateStrategy = machinesetStatusRESTStrategy{
		machinesetRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = machinesetStatusUpdateStrategy
)

// Canonicalize does not transform a machineset.
func (machinesetRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to create")
	}
}

// NamespaceScoped returns true as machinesets are scoped to a namespace.
func (machinesetRESTStrategy) NamespaceScoped() bool {
	return true
}

// PrepareForCreate receives a the incoming MachineSet and clears it's
// Status. Status is not a user settable field.
func (machinesetRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	machineset, ok := obj.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	machineset.Status = clusteroperator.MachineSetStatus{}
}

func (machinesetRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateMachineSet(obj.(*clusteroperator.MachineSet))
}

func (machinesetRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (machinesetRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (machinesetRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newMachineSet, ok := new.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to update to")
	}
	oldMachineSet, ok := old.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to update from")
	}

	newMachineSet.Status = oldMachineSet.Status
}

func (machinesetRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newMachineSet, ok := new.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to validate to")
	}
	oldMachineSet, ok := old.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to validate from")
	}

	return validation.ValidateMachineSetUpdate(newMachineSet, oldMachineSet)
}

func (machinesetStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newMachineSet, ok := new.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to update to")
	}
	oldMachineSet, ok := old.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to update from")
	}
	// status changes are not allowed to update spec
	newMachineSet.Spec = oldMachineSet.Spec
}

func (machinesetStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newMachineSet, ok := new.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to validate to")
	}
	oldMachineSet, ok := old.(*clusteroperator.MachineSet)
	if !ok {
		glog.Fatal("received a non-machineset object to validate from")
	}

	return validation.ValidateMachineSetStatusUpdate(newMachineSet, oldMachineSet)
}
