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

package cluster

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"

	controllerutil "github.com/openshift/cluster-operator/pkg/controller"
)

// MachineSetControllerRefManager is used to manage controller refs of MachineSets
type MachineSetControllerRefManager struct {
	controllerutil.BaseControllerRefManager
	controllerKind   schema.GroupVersionKind
	machineSetClient clusterapiclient.MachineSetsGetter
}

// NewMachineSetControllerRefManager returns a MachineSetControllerRefManager that exposes
// methods to manage the controllerRef of MachineSets.
//
// The CanAdopt() function can be used to perform a potentially expensive check
// (such as a live GET from the API server) prior to the first adoption.
// It will only be called (at most once) if an adoption is actually attempted.
// If CanAdopt() returns a non-nil error, all adoptions will fail.
//
// NOTE: Once CanAdopt() is called, it will not be called again by the same
//       MachineSetControllerRefManager instance. Create a new instance if it
//       makes sense to check CanAdopt() again (e.g. in a different sync pass).
func NewMachineSetControllerRefManager(
	machineSetClient clusterapiclient.MachineSetsGetter,
	controller metav1.Object,
	selector labels.Selector,
	controllerKind schema.GroupVersionKind,
	canAdopt func() error,
) *MachineSetControllerRefManager {
	return &MachineSetControllerRefManager{
		BaseControllerRefManager: controllerutil.BaseControllerRefManager{
			Controller:   controller,
			Selector:     selector,
			CanAdoptFunc: canAdopt,
		},
		controllerKind:   controllerKind,
		machineSetClient: machineSetClient,
	}
}

// ClaimMachineSets tries to take ownership of a list of MachineSets.
//
// It will reconcile the following:
//   * Adopt orphans if the selector matches.
//   * Release owned objects if the selector no longer matches.
//
// A non-nil error is returned if some form of reconciliation was attempted and
// failed. Usually, controllers should try again later in case reconciliation
// is still needed.
//
// If the error is nil, either the reconciliation succeeded, or no
// reconciliation was necessary. The list of MachineSets that you now own is
// returned.
func (m *MachineSetControllerRefManager) ClaimMachineSets(sets []*capiv1.MachineSet) ([]*capiv1.MachineSet, error) {
	var claimed []*capiv1.MachineSet
	var errlist []error

	match := func(obj metav1.Object) bool {
		return m.Selector.Matches(labels.Set(obj.GetLabels()))
	}
	adopt := func(obj metav1.Object) error {
		return m.AdoptMachineSet(obj.(*capiv1.MachineSet))
	}
	release := func(obj metav1.Object) error {
		return m.ReleaseMachineSet(obj.(*capiv1.MachineSet))
	}

	for _, rs := range sets {
		ok, err := m.ClaimObject(rs, match, adopt, release)
		if err != nil {
			errlist = append(errlist, err)
			continue
		}
		if ok {
			claimed = append(claimed, rs)
		}
	}
	return claimed, utilerrors.NewAggregate(errlist)
}

// AdoptMachineSet sends a patch to take control of the MachineSet. It returns
// the error if the patching fails.
func (m *MachineSetControllerRefManager) AdoptMachineSet(rs *capiv1.MachineSet) error {
	if err := m.CanAdopt(); err != nil {
		return fmt.Errorf("can't adopt MachineSet %v/%v (%v): %v", rs.Namespace, rs.Name, rs.UID, err)
	}
	// Note that ValidateOwnerReferences() will reject this patch if another
	// OwnerReference exists with controller=true.
	addControllerPatch := fmt.Sprintf(
		`{"metadata":{"ownerReferences":[{"apiVersion":"%s","kind":"%s","name":"%s","uid":"%s","controller":true,"blockOwnerDeletion":true}],"uid":"%s"}}`,
		m.controllerKind.GroupVersion(), m.controllerKind.Kind,
		m.Controller.GetName(), m.Controller.GetUID(), rs.UID)
	_, err := m.machineSetClient.MachineSets(rs.Namespace).Patch(rs.Name, types.StrategicMergePatchType, []byte(addControllerPatch))
	return err
}

// ReleaseMachineSet sends a patch to free the MachineSet from the control of the Deployment controller.
// It returns the error if the patching fails. 404 and 422 errors are ignored.
func (m *MachineSetControllerRefManager) ReleaseMachineSet(machineSet *capiv1.MachineSet) error {
	glog.V(2).Infof("patching MachineSet %s_%s to remove its controllerRef to %s/%s:%s",
		machineSet.Namespace, machineSet.Name, m.controllerKind.GroupVersion(), m.controllerKind.Kind, m.Controller.GetName())
	deleteOwnerRefPatch := fmt.Sprintf(`{"metadata":{"ownerReferences":[{"$patch":"delete","uid":"%s"}],"uid":"%s"}}`, m.Controller.GetUID(), machineSet.UID)
	_, err := m.machineSetClient.MachineSets(machineSet.Namespace).Patch(machineSet.Name, types.StrategicMergePatchType, []byte(deleteOwnerRefPatch))
	if err != nil {
		if errors.IsNotFound(err) {
			// If the MachineSet no longer exists, ignore it.
			return nil
		}
		if errors.IsInvalid(err) {
			// Invalid error will be returned in two cases: 1. the MachineSet
			// has no owner reference, 2. the uid of the MachineSet doesn't
			// match, which means the MachineSet is deleted and then recreated.
			// In both cases, the error can be ignored.
			return nil
		}
	}
	return err
}

// RecheckDeletionTimestamp returns a CanAdopt() function to recheck deletion.
//
// The CanAdopt() function calls getObject() to fetch the latest value,
// and denies adoption attempts if that object has a non-nil DeletionTimestamp.
func RecheckDeletionTimestamp(getObject func() (metav1.Object, error)) func() error {
	return func() error {
		obj, err := getObject()
		if err != nil {
			return fmt.Errorf("can't recheck DeletionTimestamp: %v", err)
		}
		if obj.GetDeletionTimestamp() != nil {
			return fmt.Errorf("%v/%v has just been deleted at %v", obj.GetNamespace(), obj.GetName(), obj.GetDeletionTimestamp())
		}
		return nil
	}
}
