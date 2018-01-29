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

package cluster

import (
	"github.com/openshift/cluster-operator/pkg/api"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/validation"
)

// NewScopeStrategy returns a new NamespaceScopedStrategy for clusters
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return clusterRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type clusterRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type clusterStatusRESTStrategy struct {
	clusterRESTStrategy
}

var (
	clusterRESTStrategies = clusterRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = clusterRESTStrategies
	_ rest.RESTUpdateStrategy = clusterRESTStrategies
	_ rest.RESTDeleteStrategy = clusterRESTStrategies

	clusterStatusUpdateStrategy = clusterStatusRESTStrategy{
		clusterRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = clusterStatusUpdateStrategy
)

// Canonicalize does not transform a cluster.
func (clusterRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to create")
	}
}

// NamespaceScoped returns true as clusters are scoped to a namespace.
func (clusterRESTStrategy) NamespaceScoped() bool {
	return true
}

// PrepareForCreate receives a the incoming Cluster and clears it's
// Status. Status is not a user settable field.
func (clusterRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	cluster, ok := obj.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to create")
	}

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	cluster.Generation = 1
	cluster.Status = clusteroperator.ClusterStatus{}
}

func (clusterRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateCluster(obj.(*clusteroperator.Cluster))
}

func (clusterRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (clusterRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (clusterRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newCluster, ok := new.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to update to")
	}
	oldCluster, ok := old.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to update from")
	}

	// Any changes to the spec increment the generation number, any changes to the
	// status should reflect the generation number of the corresponding object.
	// See metav1.ObjectMeta description for more information on Generation.
	if !equality.Semantic.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		newCluster.Generation = oldCluster.Generation + 1
	}

	newCluster.Status = oldCluster.Status
}

func (clusterRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newCluster, ok := new.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to validate to")
	}
	oldCluster, ok := old.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to validate from")
	}

	return validation.ValidateClusterUpdate(newCluster, oldCluster)
}

func (clusterStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newCluster, ok := new.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to update to")
	}
	oldCluster, ok := old.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to update from")
	}
	// status changes are not allowed to update spec
	newCluster.Spec = oldCluster.Spec
}

func (clusterStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newCluster, ok := new.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to validate to")
	}
	oldCluster, ok := old.(*clusteroperator.Cluster)
	if !ok {
		glog.Fatal("received a non-cluster object to validate from")
	}

	return validation.ValidateClusterStatusUpdate(newCluster, oldCluster)
}
