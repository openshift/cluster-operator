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

package ansible

import (
	"fmt"

	kapi "k8s.io/api/core/v1"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	defaultImageName = "openshift/origin-ansible"
)

// GetAnsibleImageForClusterVersion gets the openshift-ansible image and pull
// policy to use for clusters that use the specified ClusterVersion.
func GetAnsibleImageForClusterVersion(cv cov1.OpenShiftConfigVersion) (string, kapi.PullPolicy) {
	image := fmt.Sprintf("%s:%s", defaultImageName, cv.Version)
	if cv.Images.OpenshiftAnsibleImage != nil {
		image = *cv.Images.OpenshiftAnsibleImage
	}
	pullPolicy := kapi.PullAlways
	if cv.Images.OpenshiftAnsibleImagePullPolicy != nil {
		pullPolicy = *cv.Images.OpenshiftAnsibleImagePullPolicy
	}
	return image, pullPolicy
}
