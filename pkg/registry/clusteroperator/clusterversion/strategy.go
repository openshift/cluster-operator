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

package clusterversion

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

// NewScopeStrategy returns a new NamespaceScopedStrategy for clusterversions
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return clusterVersionRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type clusterVersionRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type clusterVersionStatusRESTStrategy struct {
	clusterVersionRESTStrategy
}

var (
	clusterVersionRESTStrategies = clusterVersionRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = clusterVersionRESTStrategies
	_ rest.RESTUpdateStrategy = clusterVersionRESTStrategies
	_ rest.RESTDeleteStrategy = clusterVersionRESTStrategies

	clusterVersionStatusUpdateStrategy = clusterVersionStatusRESTStrategy{
		clusterVersionRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = clusterVersionStatusUpdateStrategy
)

// Canonicalize does not transform a cluster.
func (clusterVersionRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-cluster version object to create")
	}
}

// NamespaceScoped returns true as cluster versions are scoped to a namespace.
func (clusterVersionRESTStrategy) NamespaceScoped() bool {
	return true
}

func (clusterVersionRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	cv, ok := obj.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-cluster version object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	cv.Status = coapi.ClusterVersionStatus{}
}

func (clusterVersionRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return covalidation.ValidateClusterVersion(obj.(*coapi.ClusterVersion))
}

func (clusterVersionRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (clusterVersionRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (clusterVersionRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newCV, ok := new.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-cluster version object to update to")
	}
	oldCV, ok := old.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-cluster version object to update from")
	}
	newCV.Status = oldCV.Status
}

func (clusterVersionRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newCV, ok := new.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-cluster version object to validate to")
	}
	oldCV, ok := old.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-cluster version object to validate from")
	}

	// Cluster versions are not actually mutable yet today, this will always return an error
	// if the spec changed.
	return covalidation.ValidateClusterVersionUpdate(newCV, oldCV)
}

func (clusterVersionStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newCV, ok := new.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-clusterversion object to update to")
	}
	oldCV, ok := old.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-clusterversion object to update from")
	}
	// status changes are not allowed to update the spec
	newCV.Spec = oldCV.Spec
}

func (clusterVersionStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newCV, ok := new.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-clusterversion object to validate to")
	}
	oldCV, ok := old.(*coapi.ClusterVersion)
	if !ok {
		glog.Fatal("received a non-clusterversion object to validate from")
	}

	return covalidation.ValidateClusterVersionStatusUpdate(newCV, oldCV)
}
