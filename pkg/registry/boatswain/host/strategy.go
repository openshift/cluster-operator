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

package host

import (
	"github.com/staebler/boatswain/pkg/api"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	sc "github.com/staebler/boatswain/pkg/apis/boatswain"
	"github.com/staebler/boatswain/pkg/apis/boatswain/validation"
)

// NewScopeStrategy returns a new NamespaceScopedStrategy for hosts
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return hostRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type hostRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type hostStatusRESTStrategy struct {
	hostRESTStrategy
}

var (
	hostRESTStrategies = hostRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = hostRESTStrategies
	_ rest.RESTUpdateStrategy = hostRESTStrategies
	_ rest.RESTDeleteStrategy = hostRESTStrategies

	hostStatusUpdateStrategy = hostStatusRESTStrategy{
		hostRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = hostStatusUpdateStrategy
)

// Canonicalize does not transform a host.
func (hostRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to create")
	}
}

// NamespaceScoped returns false as hosts are not scoped to a namespace.
func (hostRESTStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForCreate receives a the incoming Host and clears it's
// Status. Status is not a user settable field.
func (hostRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	host, ok := obj.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to create")
	}
	// Is there anything to pull out of the context `ctx`?

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	host.Status = boatswain.HostStatus{}
	// Fill in the first entry set to "creating"?
	host.Status.Conditions = []boatswain.ServiceBrokerCondition{}
	host.Finalizers = []string{boatswain.FinalizerServiceCatalog}
	host.Generation = 1
}

func (hostRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateHost(obj.(*boatswain.Host))
}

func (hostRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (hostRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (hostRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newHost, ok := new.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to update to")
	}
	oldHost, ok := old.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to update from")
	}

	newHost.Status = oldHost.Status

	// Ignore the RelistRequests field when it is the default value
	if newHost.Spec.RelistRequests == 0 {
		newHost.Spec.RelistRequests = oldHost.Spec.RelistRequests
	}

	// Spec updates bump the generation so that we can distinguish between
	// spec changes and other changes to the object.
	if !apiequality.Semantic.DeepEqual(oldHost.Spec, newHost.Spec) {
		newHost.Generation = oldHost.Generation + 1
	}
}

func (hostRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newHost, ok := new.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to validate to")
	}
	oldHost, ok := old.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to validate from")
	}

	return validation.ValidateHostUpdate(newHost, oldHost)
}

func (hostStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newHost, ok := new.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to update to")
	}
	oldHost, ok := old.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to update from")
	}
	// status changes are not allowed to update spec
	newHost.Spec = oldHost.Spec
}

func (hostStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newHost, ok := new.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to validate to")
	}
	oldHost, ok := old.(*boatswain.Host)
	if !ok {
		glog.Fatal("received a non-host object to validate from")
	}

	return validation.ValidateHostStatusUpdate(newHost, oldHost)
}
