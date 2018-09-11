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

package dnszone

import (
	"github.com/openshift/cluster-operator/pkg/api"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	covalidation "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/validation"
)

// NewScopeStrategy returns a new NamespaceScopedStrategy for dnszones
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return dnsZoneRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type dnsZoneRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type dnsZoneStatusRESTStrategy struct {
	dnsZoneRESTStrategy
}

var (
	dnsZoneRESTStrategies = dnsZoneRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = dnsZoneRESTStrategies
	_ rest.RESTUpdateStrategy = dnsZoneRESTStrategies
	_ rest.RESTDeleteStrategy = dnsZoneRESTStrategies

	dnsZoneStatusUpdateStrategy = dnsZoneStatusRESTStrategy{
		dnsZoneRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = dnsZoneStatusUpdateStrategy
)

// Canonicalize does not transform a dnszone.
func (dnsZoneRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to create")
	}
}

// NamespaceScoped returns true as dnszones are scoped to a namespace.
func (dnsZoneRESTStrategy) NamespaceScoped() bool {
	return true
}

func (dnsZoneRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	cv, ok := obj.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone version object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	cv.Status = coapi.DNSZoneStatus{}
}

func (dnsZoneRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return covalidation.ValidateDNSZone(obj.(*coapi.DNSZone))
}

func (dnsZoneRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (dnsZoneRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (dnsZoneRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newCV, ok := new.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to update to")
	}
	oldCV, ok := old.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to update from")
	}
	newCV.Status = oldCV.Status
}

func (dnsZoneRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newCV, ok := new.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to validate to")
	}
	oldCV, ok := old.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to validate from")
	}

	// DNSZones are not actually mutable today, this will always return an error
	// if the spec changed.
	return covalidation.ValidateDNSZoneUpdate(newCV, oldCV)
}

func (dnsZoneStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newCV, ok := new.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to update to")
	}
	oldCV, ok := old.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to update from")
	}
	// status changes are not allowed to update the spec
	newCV.Spec = oldCV.Spec
}

func (dnsZoneStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newCV, ok := new.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to validate to")
	}
	oldCV, ok := old.(*coapi.DNSZone)
	if !ok {
		glog.Fatal("received a non-dnszone object to validate from")
	}

	return covalidation.ValidateDNSZoneStatusUpdate(newCV, oldCV)
}
