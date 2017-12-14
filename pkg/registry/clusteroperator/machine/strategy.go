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

package machine

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

// NewScopeStrategy returns a new NamespaceScopedStrategy for machines
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return machineRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type machineRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type machineStatusRESTStrategy struct {
	machineRESTStrategy
}

var (
	machineRESTStrategies = machineRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = machineRESTStrategies
	_ rest.RESTUpdateStrategy = machineRESTStrategies
	_ rest.RESTDeleteStrategy = machineRESTStrategies

	machineStatusUpdateStrategy = machineStatusRESTStrategy{
		machineRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = machineStatusUpdateStrategy
)

// Canonicalize does not transform a machine.
func (machineRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to create")
	}
}

// NamespaceScoped returns true as machines are scoped to a namespace.
func (machineRESTStrategy) NamespaceScoped() bool {
	return true
}

// PrepareForCreate receives a the incoming Machine and clears it's
// Status. Status is not a user settable field.
func (machineRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	machine, ok := obj.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	machine.Status = clusteroperator.MachineStatus{}
}

func (machineRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateMachine(obj.(*clusteroperator.Machine))
}

func (machineRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (machineRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (machineRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newMachine, ok := new.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to update to")
	}
	oldMachine, ok := old.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to update from")
	}

	newMachine.Status = oldMachine.Status
}

func (machineRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newMachine, ok := new.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to validate to")
	}
	oldMachine, ok := old.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to validate from")
	}

	return validation.ValidateMachineUpdate(newMachine, oldMachine)
}

func (machineStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newMachine, ok := new.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to update to")
	}
	oldMachine, ok := old.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to update from")
	}
	// status changes are not allowed to update spec
	newMachine.Spec = oldMachine.Spec
}

func (machineStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newMachine, ok := new.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to validate to")
	}
	oldMachine, ok := old.(*clusteroperator.Machine)
	if !ok {
		glog.Fatal("received a non-machine object to validate from")
	}

	return validation.ValidateMachineStatusUpdate(newMachine, oldMachine)
}
