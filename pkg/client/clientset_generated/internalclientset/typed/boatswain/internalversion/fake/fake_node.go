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

package fake

import (
	boatswain "github.com/staebler/boatswain/pkg/apis/boatswain"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeNodes implements NodeInterface
type FakeNodes struct {
	Fake *FakeBoatswain
	ns   string
}

var nodesResource = schema.GroupVersionResource{Group: "boatswain.openshift.io", Version: "", Resource: "nodes"}

var nodesKind = schema.GroupVersionKind{Group: "boatswain.openshift.io", Version: "", Kind: "Node"}

// Get takes name of the node, and returns the corresponding node object, and an error if there is any.
func (c *FakeNodes) Get(name string, options v1.GetOptions) (result *boatswain.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(nodesResource, c.ns, name), &boatswain.Node{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Node), err
}

// List takes label and field selectors, and returns the list of Nodes that match those selectors.
func (c *FakeNodes) List(opts v1.ListOptions) (result *boatswain.NodeList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(nodesResource, nodesKind, c.ns, opts), &boatswain.NodeList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &boatswain.NodeList{}
	for _, item := range obj.(*boatswain.NodeList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested nodes.
func (c *FakeNodes) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(nodesResource, c.ns, opts))

}

// Create takes the representation of a node and creates it.  Returns the server's representation of the node, and an error, if there is any.
func (c *FakeNodes) Create(node *boatswain.Node) (result *boatswain.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(nodesResource, c.ns, node), &boatswain.Node{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Node), err
}

// Update takes the representation of a node and updates it. Returns the server's representation of the node, and an error, if there is any.
func (c *FakeNodes) Update(node *boatswain.Node) (result *boatswain.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(nodesResource, c.ns, node), &boatswain.Node{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Node), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeNodes) UpdateStatus(node *boatswain.Node) (*boatswain.Node, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(nodesResource, "status", c.ns, node), &boatswain.Node{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Node), err
}

// Delete takes name of the node and deletes it. Returns an error if one occurs.
func (c *FakeNodes) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(nodesResource, c.ns, name), &boatswain.Node{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNodes) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(nodesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &boatswain.NodeList{})
	return err
}

// Patch applies the patch and returns the patched node.
func (c *FakeNodes) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *boatswain.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(nodesResource, c.ns, name, data, subresources...), &boatswain.Node{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Node), err
}
