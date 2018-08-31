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

package validation

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/request"

	"github.com/golang/glog"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/builders"
	"k8s.io/apimachinery/pkg/util/validation/field"
	capicluster "sigs.k8s.io/cluster-api/pkg/apis/cluster"
)

// MachineStrategy is the strategy that the API server will use for Machine
// resources.
type MachineStrategy struct {
	builders.StorageBuilder
}

// Validate checks that an instance of Machine is well formed
func (m MachineStrategy) Validate(ctx request.Context, obj runtime.Object) field.ErrorList {
	errors := field.ErrorList{}
	errors = append(errors, m.StorageBuilder.Validate(ctx, obj)...)
	machine := obj.(*capicluster.Machine)
	glog.Infof("Custom validation of Machine %s", machine.Name)
	return errors
}

// PrepareForCreate clears fields that are not allowed to be set by end users on creation.
func (m MachineStrategy) PrepareForCreate(ctx request.Context, obj runtime.Object) {
	m.StorageBuilder.PrepareForCreate(ctx, obj)
}

// ReplaceMachineStorageBuilder replaces the storage builder used upstream for
// Machine resources with a custom MachineStrategy builder.
func ReplaceMachineStorageBuilder(storageBuilder builders.StorageBuilder) builders.StorageBuilder {
	return &MachineStrategy{storageBuilder}
}
