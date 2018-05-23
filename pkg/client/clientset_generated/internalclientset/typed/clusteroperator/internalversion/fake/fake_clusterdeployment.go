/*
Copyright 2018 The Kubernetes Authors.

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

package fake

import (
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeClusterDeployments implements ClusterDeploymentInterface
type FakeClusterDeployments struct {
	Fake *FakeClusteroperator
	ns   string
}

var clusterdeploymentsResource = schema.GroupVersionResource{Group: "clusteroperator.openshift.io", Version: "", Resource: "clusterdeployments"}

var clusterdeploymentsKind = schema.GroupVersionKind{Group: "clusteroperator.openshift.io", Version: "", Kind: "ClusterDeployment"}

// Get takes name of the clusterDeployment, and returns the corresponding clusterDeployment object, and an error if there is any.
func (c *FakeClusterDeployments) Get(name string, options v1.GetOptions) (result *clusteroperator.ClusterDeployment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(clusterdeploymentsResource, c.ns, name), &clusteroperator.ClusterDeployment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.ClusterDeployment), err
}

// List takes label and field selectors, and returns the list of ClusterDeployments that match those selectors.
func (c *FakeClusterDeployments) List(opts v1.ListOptions) (result *clusteroperator.ClusterDeploymentList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(clusterdeploymentsResource, clusterdeploymentsKind, c.ns, opts), &clusteroperator.ClusterDeploymentList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &clusteroperator.ClusterDeploymentList{}
	for _, item := range obj.(*clusteroperator.ClusterDeploymentList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested clusterDeployments.
func (c *FakeClusterDeployments) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(clusterdeploymentsResource, c.ns, opts))

}

// Create takes the representation of a clusterDeployment and creates it.  Returns the server's representation of the clusterDeployment, and an error, if there is any.
func (c *FakeClusterDeployments) Create(clusterDeployment *clusteroperator.ClusterDeployment) (result *clusteroperator.ClusterDeployment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(clusterdeploymentsResource, c.ns, clusterDeployment), &clusteroperator.ClusterDeployment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.ClusterDeployment), err
}

// Update takes the representation of a clusterDeployment and updates it. Returns the server's representation of the clusterDeployment, and an error, if there is any.
func (c *FakeClusterDeployments) Update(clusterDeployment *clusteroperator.ClusterDeployment) (result *clusteroperator.ClusterDeployment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(clusterdeploymentsResource, c.ns, clusterDeployment), &clusteroperator.ClusterDeployment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.ClusterDeployment), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeClusterDeployments) UpdateStatus(clusterDeployment *clusteroperator.ClusterDeployment) (*clusteroperator.ClusterDeployment, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(clusterdeploymentsResource, "status", c.ns, clusterDeployment), &clusteroperator.ClusterDeployment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.ClusterDeployment), err
}

// Delete takes name of the clusterDeployment and deletes it. Returns an error if one occurs.
func (c *FakeClusterDeployments) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(clusterdeploymentsResource, c.ns, name), &clusteroperator.ClusterDeployment{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeClusterDeployments) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(clusterdeploymentsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &clusteroperator.ClusterDeploymentList{})
	return err
}

// Patch applies the patch and returns the patched clusterDeployment.
func (c *FakeClusterDeployments) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.ClusterDeployment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(clusterdeploymentsResource, c.ns, name, data, subresources...), &clusteroperator.ClusterDeployment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.ClusterDeployment), err
}
