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

package clusterdeployment

import (
	"github.com/openshift/cluster-operator/pkg/api"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/golang/glog"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/validation"
)

// NewScopeStrategy returns a new NamespaceScopedStrategy for clusterdeployments
func NewScopeStrategy() rest.NamespaceScopedStrategy {
	return clusterDeploymentRESTStrategies
}

// implements interfaces RESTCreateStrategy, RESTUpdateStrategy, RESTDeleteStrategy,
// NamespaceScopedStrategy
type clusterDeploymentRESTStrategy struct {
	runtime.ObjectTyper // inherit ObjectKinds method
	names.NameGenerator // GenerateName method for CreateStrategy
}

// implements interface RESTUpdateStrategy
type clusterDeploymentStatusRESTStrategy struct {
	clusterDeploymentRESTStrategy
}

var (
	clusterDeploymentRESTStrategies = clusterDeploymentRESTStrategy{
		// embeds to pull in existing code behavior from upstream

		// this has an interesting NOTE on it. Not sure if it applies to us.
		ObjectTyper: api.Scheme,
		// use the generator from upstream k8s, or implement method
		// `GenerateName(base string) string`
		NameGenerator: names.SimpleNameGenerator,
	}
	_ rest.RESTCreateStrategy = clusterDeploymentRESTStrategies
	_ rest.RESTUpdateStrategy = clusterDeploymentRESTStrategies
	_ rest.RESTDeleteStrategy = clusterDeploymentRESTStrategies

	clusterDeploymentStatusUpdateStrategy = clusterDeploymentStatusRESTStrategy{
		clusterDeploymentRESTStrategies,
	}
	_ rest.RESTUpdateStrategy = clusterDeploymentStatusUpdateStrategy
)

// Canonicalize does not transform a clusterdeployment.
func (clusterDeploymentRESTStrategy) Canonicalize(obj runtime.Object) {
	_, ok := obj.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to create")
	}
}

// NamespaceScoped returns true as clusterdeployments are scoped to a namespace.
func (clusterDeploymentRESTStrategy) NamespaceScoped() bool {
	return true
}

// PrepareForCreate receives a the incoming ClusterDeployment and clears it's
// Status. Status is not a user settable field.
func (clusterDeploymentRESTStrategy) PrepareForCreate(ctx genericapirequest.Context, obj runtime.Object) {
	clusterDeployment, ok := obj.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-cluster object to create")
	}

	clusterDeployment.Spec.ClusterName = clusterDeployment.Name + "-" + utilrand.String(5)

	// Creating a brand new object, thus it must have no
	// status. We can't fail here if they passed a status in, so
	// we just wipe it clean.
	clusterDeployment.Status = clusteroperator.ClusterDeploymentStatus{}

	clusterDeployment.Generation = 1
}

func (clusterDeploymentRESTStrategy) Validate(ctx genericapirequest.Context, obj runtime.Object) field.ErrorList {
	return validation.ValidateClusterDeployment(obj.(*clusteroperator.ClusterDeployment))
}

func (clusterDeploymentRESTStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (clusterDeploymentRESTStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (clusterDeploymentRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newClusterDeployment, ok := new.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to update to")
	}
	oldClusterDeployment, ok := old.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to update from")
	}

	// Any changes to the spec increment the generation number, any changes to the
	// status should reflect the generation number of the corresponding object.
	// See metav1.ObjectMeta description for more information on Generation.
	if !equality.Semantic.DeepEqual(oldClusterDeployment.Spec, newClusterDeployment.Spec) {
		newClusterDeployment.Generation = oldClusterDeployment.Generation + 1
	}

	newClusterDeployment.Status = oldClusterDeployment.Status
}

func (clusterDeploymentRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newClusterDeployment, ok := new.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to validate to")
	}
	oldCluster, ok := old.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to validate from")
	}

	return validation.ValidateClusterDeploymentUpdate(newClusterDeployment, oldCluster)
}

func (clusterDeploymentStatusRESTStrategy) PrepareForUpdate(ctx genericapirequest.Context, new, old runtime.Object) {
	newClusterDeployment, ok := new.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to update to")
	}
	oldClusterDeployment, ok := old.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to update from")
	}
	// status changes are not allowed to update spec
	newClusterDeployment.Spec = oldClusterDeployment.Spec
}

func (clusterDeploymentStatusRESTStrategy) ValidateUpdate(ctx genericapirequest.Context, new, old runtime.Object) field.ErrorList {
	newClusterDeployment, ok := new.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to validate to")
	}
	oldClusterDeployment, ok := old.(*clusteroperator.ClusterDeployment)
	if !ok {
		glog.Fatal("received a non-clusterdeployment object to validate from")
	}

	return validation.ValidateClusterDeploymentStatusUpdate(newClusterDeployment, oldClusterDeployment)
}
