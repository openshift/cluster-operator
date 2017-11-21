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

// FakeHosts implements HostInterface
type FakeHosts struct {
	Fake *FakeBoatswain
}

var hostsResource = schema.GroupVersionResource{Group: "boatswain.openshift.io", Version: "", Resource: "hosts"}

var hostsKind = schema.GroupVersionKind{Group: "boatswain.openshift.io", Version: "", Kind: "Host"}

// Get takes name of the host, and returns the corresponding host object, and an error if there is any.
func (c *FakeHosts) Get(name string, options v1.GetOptions) (result *boatswain.Host, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(hostsResource, name), &boatswain.Host{})
	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Host), err
}

// List takes label and field selectors, and returns the list of Hosts that match those selectors.
func (c *FakeHosts) List(opts v1.ListOptions) (result *boatswain.HostList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(hostsResource, hostsKind, opts), &boatswain.HostList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &boatswain.HostList{}
	for _, item := range obj.(*boatswain.HostList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested hosts.
func (c *FakeHosts) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(hostsResource, opts))
}

// Create takes the representation of a host and creates it.  Returns the server's representation of the host, and an error, if there is any.
func (c *FakeHosts) Create(host *boatswain.Host) (result *boatswain.Host, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(hostsResource, host), &boatswain.Host{})
	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Host), err
}

// Update takes the representation of a host and updates it. Returns the server's representation of the host, and an error, if there is any.
func (c *FakeHosts) Update(host *boatswain.Host) (result *boatswain.Host, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(hostsResource, host), &boatswain.Host{})
	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Host), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeHosts) UpdateStatus(host *boatswain.Host) (*boatswain.Host, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(hostsResource, "status", host), &boatswain.Host{})
	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Host), err
}

// Delete takes name of the host and deletes it. Returns an error if one occurs.
func (c *FakeHosts) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(hostsResource, name), &boatswain.Host{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeHosts) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(hostsResource, listOptions)

	_, err := c.Fake.Invokes(action, &boatswain.HostList{})
	return err
}

// Patch applies the patch and returns the patched host.
func (c *FakeHosts) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *boatswain.Host, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(hostsResource, name, data, subresources...), &boatswain.Host{})
	if obj == nil {
		return nil, err
	}
	return obj.(*boatswain.Host), err
}
