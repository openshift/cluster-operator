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
	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeMachines implements MachineInterface
type FakeMachines struct {
	Fake *FakeClusteroperator
	ns   string
}

var machinesResource = schema.GroupVersionResource{Group: "clusteroperator.openshift.io", Version: "", Resource: "machines"}

var machinesKind = schema.GroupVersionKind{Group: "clusteroperator.openshift.io", Version: "", Kind: "Machine"}

// Get takes name of the machine, and returns the corresponding machine object, and an error if there is any.
func (c *FakeMachines) Get(name string, options v1.GetOptions) (result *clusteroperator.Machine, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(machinesResource, c.ns, name), &clusteroperator.Machine{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.Machine), err
}

// List takes label and field selectors, and returns the list of Machines that match those selectors.
func (c *FakeMachines) List(opts v1.ListOptions) (result *clusteroperator.MachineList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(machinesResource, machinesKind, c.ns, opts), &clusteroperator.MachineList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &clusteroperator.MachineList{}
	for _, item := range obj.(*clusteroperator.MachineList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested machines.
func (c *FakeMachines) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(machinesResource, c.ns, opts))

}

// Create takes the representation of a machine and creates it.  Returns the server's representation of the machine, and an error, if there is any.
func (c *FakeMachines) Create(machine *clusteroperator.Machine) (result *clusteroperator.Machine, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(machinesResource, c.ns, machine), &clusteroperator.Machine{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.Machine), err
}

// Update takes the representation of a machine and updates it. Returns the server's representation of the machine, and an error, if there is any.
func (c *FakeMachines) Update(machine *clusteroperator.Machine) (result *clusteroperator.Machine, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(machinesResource, c.ns, machine), &clusteroperator.Machine{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.Machine), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeMachines) UpdateStatus(machine *clusteroperator.Machine) (*clusteroperator.Machine, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(machinesResource, "status", c.ns, machine), &clusteroperator.Machine{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.Machine), err
}

// Delete takes name of the machine and deletes it. Returns an error if one occurs.
func (c *FakeMachines) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(machinesResource, c.ns, name), &clusteroperator.Machine{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeMachines) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(machinesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &clusteroperator.MachineList{})
	return err
}

// Patch applies the patch and returns the patched machine.
func (c *FakeMachines) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *clusteroperator.Machine, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(machinesResource, c.ns, name, data, subresources...), &clusteroperator.Machine{})

	if obj == nil {
		return nil, err
	}
	return obj.(*clusteroperator.Machine), err
}
