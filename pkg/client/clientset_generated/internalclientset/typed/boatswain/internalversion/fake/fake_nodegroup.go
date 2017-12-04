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

// FakeNodeGroups implements NodeGroupInterface
type FakeNodeGroups struct {
	Fake *FakeBoatswain
	ns   string
}

var nodegroupsResource = schema.GroupVersionResource{Group: "boatswain.openshift.io", Version: "", Resource: "nodegroups"}

var nodegroupsKind = schema.GroupVersionKind{Group: "boatswain.openshift.io", Version: "", Kind: "NodeGroup"}

// Get takes name of the nodeGroup, and returns the corresponding nodeGroup object, and an error if there is any.
func (c *FakeNodeGroups) Get(name string, options v1.GetOptions) (result *boatswain.NodeGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(nodegroupsResource, c.ns, name), &boatswain.NodeGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.NodeGroup), err
}

// List takes label and field selectors, and returns the list of NodeGroups that match those selectors.
func (c *FakeNodeGroups) List(opts v1.ListOptions) (result *boatswain.NodeGroupList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(nodegroupsResource, nodegroupsKind, c.ns, opts), &boatswain.NodeGroupList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &boatswain.NodeGroupList{}
	for _, item := range obj.(*boatswain.NodeGroupList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested nodeGroups.
func (c *FakeNodeGroups) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(nodegroupsResource, c.ns, opts))

}

// Create takes the representation of a nodeGroup and creates it.  Returns the server's representation of the nodeGroup, and an error, if there is any.
func (c *FakeNodeGroups) Create(nodeGroup *boatswain.NodeGroup) (result *boatswain.NodeGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(nodegroupsResource, c.ns, nodeGroup), &boatswain.NodeGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.NodeGroup), err
}

// Update takes the representation of a nodeGroup and updates it. Returns the server's representation of the nodeGroup, and an error, if there is any.
func (c *FakeNodeGroups) Update(nodeGroup *boatswain.NodeGroup) (result *boatswain.NodeGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(nodegroupsResource, c.ns, nodeGroup), &boatswain.NodeGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.NodeGroup), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeNodeGroups) UpdateStatus(nodeGroup *boatswain.NodeGroup) (*boatswain.NodeGroup, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(nodegroupsResource, "status", c.ns, nodeGroup), &boatswain.NodeGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.NodeGroup), err
}

// Delete takes name of the nodeGroup and deletes it. Returns an error if one occurs.
func (c *FakeNodeGroups) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(nodegroupsResource, c.ns, name), &boatswain.NodeGroup{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNodeGroups) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(nodegroupsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &boatswain.NodeGroupList{})
	return err
}

// Patch applies the patch and returns the patched nodeGroup.
func (c *FakeNodeGroups) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *boatswain.NodeGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(nodegroupsResource, c.ns, name, data, subresources...), &boatswain.NodeGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.NodeGroup), err
}
