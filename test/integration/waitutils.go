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

package integration

import (
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	cocontroller "github.com/openshift/cluster-operator/pkg/controller"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

func waitForObjectStatus(namespace, name string, getFromStore func(namespace, name string) (metav1.Object, error), checkStatus func(metav1.Object) bool) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout,
		func() (bool, error) {
			obj, err := getFromStore(namespace, name)
			if err != nil {
				return false, nil
			}
			return checkStatus(obj), nil
		},
	)
}

// waitForObjectToNotExist waits for the object with the specified name to not
// exist in the specified namespace.
func waitForObjectToNotExist(namespace, name string, getFromStore func(namespace, name string) (metav1.Object, error)) error {
	return waitForObjectStatus(
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			obj, err := getFromStore(namespace, name)
			if errors.IsNotFound(err) {
				return nil, nil
			}
			return obj, err
		},
		func(obj metav1.Object) bool { return obj == nil },
	)
}

// waitForObjectToNotExistOrNotHaveFinalizer waits for the object with the
// specified namespace and name to either not exist or to not have the
// specified finalizer.
func waitForObjectToNotExistOrNotHaveFinalizer(namespace, name string, finalizer string, getFromStore func(namespace, name string) (metav1.Object, error)) error {
	return waitForObjectStatus(
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			obj, err := getFromStore(namespace, name)
			if errors.IsNotFound(err) {
				return nil, nil
			}
			return obj, err
		},
		func(obj metav1.Object) bool {
			return obj == nil || !sets.NewString(obj.GetFinalizers()...).Has(finalizer)
		},
	)
}

func waitForClusterStatus(capiClient capiclientset.Interface, namespace, name string, checkStatus func(*capiv1.Cluster) bool) error {
	return waitForObjectStatus(
		namespace,
		name,
		func(namespace, name string) (metav1.Object, error) {
			return getCluster(capiClient, namespace, name)
		},
		func(obj metav1.Object) bool { return checkStatus(obj.(*capiv1.Cluster)) },
	)
}

func waitForMachineSetStatus(capiClient capiclientset.Interface, namespace, name string, checkStatus func(*capiv1.MachineSet) bool) error {
	return waitForObjectStatus(
		namespace,
		name,
		func(namespace, name string) (metav1.Object, error) {
			return getMachineSet(capiClient, namespace, name)
		},
		func(obj metav1.Object) bool { return checkStatus(obj.(*capiv1.MachineSet)) },
	)
}

// waitForClusterToExist waits for the Cluster with the specified name to
// exist in the specified namespace.
func waitForClusterToExist(capiClient capiclientset.Interface, namespace, name string) error {
	return waitForClusterStatus(
		capiClient,
		namespace, name,
		func(cluster *capiv1.Cluster) bool { return cluster != nil },
	)
}

// waitForClusterToNotExist waits for the Cluster with the specified name to not
// exist in the specified namespace.
func waitForClusterToNotExist(capiClient capiclientset.Interface, namespace, name string) error {
	return waitForObjectToNotExist(
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			return getCluster(capiClient, namespace, name)
		},
	)
}

// waitForMachineSetToExist waits for the MachineSet with the specified name to
// exist in the specified namespace.
func waitForMachineSetToExist(capiClient capiclientset.Interface, namespace, name string) error {
	return waitForMachineSetStatus(
		capiClient,
		namespace, name,
		func(machineSet *capiv1.MachineSet) bool { return machineSet != nil },
	)
}

// waitForMachineSetToNotExist waits for the MachineSet with the specified name to not
// exist in the specified namespace.
func waitForMachineSetToNotExist(capiClient capiclientset.Interface, namespace, name string) error {
	return waitForObjectToNotExist(
		namespace, name,
		func(namespace, name string) (metav1.Object, error) {
			return getMachineSet(capiClient, namespace, name)
		},
	)
}

// waitForClusterProvisioned waits for the cluster to be provisioned.
func waitForClusterProvisioned(capiClient capiclientset.Interface, namespace, name string) error {
	return waitForClusterStatus(
		capiClient,
		namespace, name,
		func(cluster *capiv1.Cluster) bool {
			status, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
			if err != nil {
				return false
			}
			return status.Provisioned
		},
	)
}

// waitForClusterReady waits for the cluster to be ready.
func waitForClusterReady(capiClient capiclientset.Interface, namespace, name string) error {
	return waitForClusterStatus(
		capiClient,
		namespace, name,
		func(cluster *capiv1.Cluster) bool {
			status, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
			if err != nil {
				return false
			}
			return status.Ready
		},
	)
}
