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

package v1alpha1

import (
	v1alpha1 "github.com/staebler/boatswain/pkg/apis/boatswain/v1alpha1"
	scheme "github.com/staebler/boatswain/pkg/client/clientset_generated/clientset/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// HostsGetter has a method to return a HostInterface.
// A group's client should implement this interface.
type HostsGetter interface {
	Hosts() HostInterface
}

// HostInterface has methods to work with Host resources.
type HostInterface interface {
	Create(*v1alpha1.Host) (*v1alpha1.Host, error)
	Update(*v1alpha1.Host) (*v1alpha1.Host, error)
	UpdateStatus(*v1alpha1.Host) (*v1alpha1.Host, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.Host, error)
	List(opts v1.ListOptions) (*v1alpha1.HostList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.Host, err error)
	HostExpansion
}

// hosts implements HostInterface
type hosts struct {
	client rest.Interface
}

// newHosts returns a Hosts
func newHosts(c *BoatswainV1alpha1Client) *hosts {
	return &hosts{
		client: c.RESTClient(),
	}
}

// Get takes name of the host, and returns the corresponding host object, and an error if there is any.
func (c *hosts) Get(name string, options v1.GetOptions) (result *v1alpha1.Host, err error) {
	result = &v1alpha1.Host{}
	err = c.client.Get().
		Resource("hosts").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Hosts that match those selectors.
func (c *hosts) List(opts v1.ListOptions) (result *v1alpha1.HostList, err error) {
	result = &v1alpha1.HostList{}
	err = c.client.Get().
		Resource("hosts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested hosts.
func (c *hosts) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Resource("hosts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a host and creates it.  Returns the server's representation of the host, and an error, if there is any.
func (c *hosts) Create(host *v1alpha1.Host) (result *v1alpha1.Host, err error) {
	result = &v1alpha1.Host{}
	err = c.client.Post().
		Resource("hosts").
		Body(host).
		Do().
		Into(result)
	return
}

// Update takes the representation of a host and updates it. Returns the server's representation of the host, and an error, if there is any.
func (c *hosts) Update(host *v1alpha1.Host) (result *v1alpha1.Host, err error) {
	result = &v1alpha1.Host{}
	err = c.client.Put().
		Resource("hosts").
		Name(host.Name).
		Body(host).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *hosts) UpdateStatus(host *v1alpha1.Host) (result *v1alpha1.Host, err error) {
	result = &v1alpha1.Host{}
	err = c.client.Put().
		Resource("hosts").
		Name(host.Name).
		SubResource("status").
		Body(host).
		Do().
		Into(result)
	return
}

// Delete takes name of the host and deletes it. Returns an error if one occurs.
func (c *hosts) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Resource("hosts").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *hosts) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Resource("hosts").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched host.
func (c *hosts) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.Host, err error) {
	result = &v1alpha1.Host{}
	err = c.client.Patch(pt).
		Resource("hosts").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
