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

package internalversion

import (
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	scheme "github.com/openshift/cluster-operator/pkg/client/clientset_generated/internalclientset/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// NodeGroupsGetter has a method to return a NodeGroupInterface.
// A group's client should implement this interface.
type NodeGroupsGetter interface {
	NodeGroups(namespace string) NodeGroupInterface
}

// NodeGroupInterface has methods to work with NodeGroup resources.
type NodeGroupInterface interface {
	Create(*clusteroperator.NodeGroup) (*clusteroperator.NodeGroup, error)
	Update(*clusteroperator.NodeGroup) (*clusteroperator.NodeGroup, error)
	UpdateStatus(*clusteroperator.NodeGroup) (*clusteroperator.NodeGroup, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*clusteroperator.NodeGroup, error)
	List(opts v1.ListOptions) (*clusteroperator.NodeGroupList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.NodeGroup, err error)
	NodeGroupExpansion
}

// nodeGroups implements NodeGroupInterface
type nodeGroups struct {
	client rest.Interface
	ns     string
}

// newNodeGroups returns a NodeGroups
func newNodeGroups(c *ClusteroperatorClient, namespace string) *nodeGroups {
	return &nodeGroups{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the nodeGroup, and returns the corresponding nodeGroup object, and an error if there is any.
func (c *nodeGroups) Get(name string, options v1.GetOptions) (result *clusteroperator.NodeGroup, err error) {
	result = &clusteroperator.NodeGroup{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("nodegroups").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of NodeGroups that match those selectors.
func (c *nodeGroups) List(opts v1.ListOptions) (result *clusteroperator.NodeGroupList, err error) {
	result = &clusteroperator.NodeGroupList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("nodegroups").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested nodeGroups.
func (c *nodeGroups) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("nodegroups").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a nodeGroup and creates it.  Returns the server's representation of the nodeGroup, and an error, if there is any.
func (c *nodeGroups) Create(nodeGroup *clusteroperator.NodeGroup) (result *clusteroperator.NodeGroup, err error) {
	result = &clusteroperator.NodeGroup{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("nodegroups").
		Body(nodeGroup).
		Do().
		Into(result)
	return
}

// Update takes the representation of a nodeGroup and updates it. Returns the server's representation of the nodeGroup, and an error, if there is any.
func (c *nodeGroups) Update(nodeGroup *clusteroperator.NodeGroup) (result *clusteroperator.NodeGroup, err error) {
	result = &clusteroperator.NodeGroup{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("nodegroups").
		Name(nodeGroup.Name).
		Body(nodeGroup).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *nodeGroups) UpdateStatus(nodeGroup *clusteroperator.NodeGroup) (result *clusteroperator.NodeGroup, err error) {
	result = &clusteroperator.NodeGroup{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("nodegroups").
		Name(nodeGroup.Name).
		SubResource("status").
		Body(nodeGroup).
		Do().
		Into(result)
	return
}

// Delete takes name of the nodeGroup and deletes it. Returns an error if one occurs.
func (c *nodeGroups) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("nodegroups").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *nodeGroups) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("nodegroups").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched nodeGroup.
func (c *nodeGroups) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.NodeGroup, err error) {
	result = &clusteroperator.NodeGroup{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("nodegroups").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
