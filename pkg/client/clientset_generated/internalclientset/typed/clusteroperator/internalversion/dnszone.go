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

package internalversion

import (
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	scheme "github.com/openshift/cluster-operator/pkg/client/clientset_generated/internalclientset/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// DNSZonesGetter has a method to return a DNSZoneInterface.
// A group's client should implement this interface.
type DNSZonesGetter interface {
	DNSZones(namespace string) DNSZoneInterface
}

// DNSZoneInterface has methods to work with DNSZone resources.
type DNSZoneInterface interface {
	Create(*clusteroperator.DNSZone) (*clusteroperator.DNSZone, error)
	Update(*clusteroperator.DNSZone) (*clusteroperator.DNSZone, error)
	UpdateStatus(*clusteroperator.DNSZone) (*clusteroperator.DNSZone, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*clusteroperator.DNSZone, error)
	List(opts v1.ListOptions) (*clusteroperator.DNSZoneList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.DNSZone, err error)
	DNSZoneExpansion
}

// dNSZones implements DNSZoneInterface
type dNSZones struct {
	client rest.Interface
	ns     string
}

// newDNSZones returns a DNSZones
func newDNSZones(c *ClusteroperatorClient, namespace string) *dNSZones {
	return &dNSZones{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the dNSZone, and returns the corresponding dNSZone object, and an error if there is any.
func (c *dNSZones) Get(name string, options v1.GetOptions) (result *clusteroperator.DNSZone, err error) {
	result = &clusteroperator.DNSZone{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("dnszones").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of DNSZones that match those selectors.
func (c *dNSZones) List(opts v1.ListOptions) (result *clusteroperator.DNSZoneList, err error) {
	result = &clusteroperator.DNSZoneList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("dnszones").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested dNSZones.
func (c *dNSZones) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("dnszones").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a dNSZone and creates it.  Returns the server's representation of the dNSZone, and an error, if there is any.
func (c *dNSZones) Create(dNSZone *clusteroperator.DNSZone) (result *clusteroperator.DNSZone, err error) {
	result = &clusteroperator.DNSZone{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("dnszones").
		Body(dNSZone).
		Do().
		Into(result)
	return
}

// Update takes the representation of a dNSZone and updates it. Returns the server's representation of the dNSZone, and an error, if there is any.
func (c *dNSZones) Update(dNSZone *clusteroperator.DNSZone) (result *clusteroperator.DNSZone, err error) {
	result = &clusteroperator.DNSZone{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("dnszones").
		Name(dNSZone.Name).
		Body(dNSZone).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *dNSZones) UpdateStatus(dNSZone *clusteroperator.DNSZone) (result *clusteroperator.DNSZone, err error) {
	result = &clusteroperator.DNSZone{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("dnszones").
		Name(dNSZone.Name).
		SubResource("status").
		Body(dNSZone).
		Do().
		Into(result)
	return
}

// Delete takes name of the dNSZone and deletes it. Returns an error if one occurs.
func (c *dNSZones) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("dnszones").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *dNSZones) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("dnszones").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched dNSZone.
func (c *dNSZones) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.DNSZone, err error) {
	result = &clusteroperator.DNSZone{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("dnszones").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
