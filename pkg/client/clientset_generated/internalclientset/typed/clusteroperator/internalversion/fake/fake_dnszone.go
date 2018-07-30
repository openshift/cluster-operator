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

// FakeDNSZones implements DNSZoneInterface
type FakeDNSZones struct {
	Fake *FakeClusteroperator
	ns   string
}

var dnszonesResource = schema.GroupVersionResource{Group: "clusteroperator.openshift.io", Version: "", Resource: "dnszones"}

var dnszonesKind = schema.GroupVersionKind{Group: "clusteroperator.openshift.io", Version: "", Kind: "DNSZone"}

// Get takes name of the dNSZone, and returns the corresponding dNSZone object, and an error if there is any.
func (c *FakeDNSZones) Get(name string, options v1.GetOptions) (result *clusteroperator.DNSZone, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(dnszonesResource, c.ns, name), &clusteroperator.DNSZone{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.DNSZone), err
}

// List takes label and field selectors, and returns the list of DNSZones that match those selectors.
func (c *FakeDNSZones) List(opts v1.ListOptions) (result *clusteroperator.DNSZoneList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(dnszonesResource, dnszonesKind, c.ns, opts), &clusteroperator.DNSZoneList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &clusteroperator.DNSZoneList{}
	for _, item := range obj.(*clusteroperator.DNSZoneList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested dNSZones.
func (c *FakeDNSZones) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(dnszonesResource, c.ns, opts))

}

// Create takes the representation of a dNSZone and creates it.  Returns the server's representation of the dNSZone, and an error, if there is any.
func (c *FakeDNSZones) Create(dNSZone *clusteroperator.DNSZone) (result *clusteroperator.DNSZone, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(dnszonesResource, c.ns, dNSZone), &clusteroperator.DNSZone{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.DNSZone), err
}

// Update takes the representation of a dNSZone and updates it. Returns the server's representation of the dNSZone, and an error, if there is any.
func (c *FakeDNSZones) Update(dNSZone *clusteroperator.DNSZone) (result *clusteroperator.DNSZone, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(dnszonesResource, c.ns, dNSZone), &clusteroperator.DNSZone{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.DNSZone), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeDNSZones) UpdateStatus(dNSZone *clusteroperator.DNSZone) (*clusteroperator.DNSZone, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(dnszonesResource, "status", c.ns, dNSZone), &clusteroperator.DNSZone{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.DNSZone), err
}

// Delete takes name of the dNSZone and deletes it. Returns an error if one occurs.
func (c *FakeDNSZones) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(dnszonesResource, c.ns, name), &clusteroperator.DNSZone{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeDNSZones) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(dnszonesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &clusteroperator.DNSZoneList{})
	return err
}

// Patch applies the patch and returns the patched dNSZone.
func (c *FakeDNSZones) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.DNSZone, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(dnszonesResource, c.ns, name, data, subresources...), &clusteroperator.DNSZone{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.DNSZone), err
}
