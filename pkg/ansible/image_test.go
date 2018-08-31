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
	"testing"

	kapi "k8s.io/api/core/v1"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

func TestGetAnsibleImageForClusterVersion(t *testing.T) {
	cases := []struct {
		name               string
		image              *string
		pullPolicy         *kapi.PullPolicy
		expectedImage      string
		expectedPullPolicy kapi.PullPolicy
	}{
		{
			name:               "defaults",
			expectedImage:      "openshift/origin-ansible:latest",
			expectedPullPolicy: kapi.PullAlways,
		},
		{
			name:               "custom image",
			image:              func(s string) *string { return &s }("custom/image:tag"),
			expectedImage:      "custom/image:tag",
			expectedPullPolicy: kapi.PullAlways,
		},
		{
			name:               "custom pull policy",
			pullPolicy:         func(p kapi.PullPolicy) *kapi.PullPolicy { return &p }(kapi.PullNever),
			expectedImage:      "openshift/origin-ansible:latest",
			expectedPullPolicy: kapi.PullNever,
		},
	}
	for _, tc := range cases {
		cv := cov1.OpenShiftConfigVersion{
			Version: "latest",
			Images: cov1.ClusterVersionImages{
				OpenshiftAnsibleImage:           tc.image,
				OpenshiftAnsibleImagePullPolicy: tc.pullPolicy,
			},
		}
		actualImage, actualPullPolicy := GetAnsibleImageForClusterVersion(cv)
		if e, a := tc.expectedImage, actualImage; e != a {
			t.Errorf("unexpected image: expected %v, got %v", e, a)
		}
		if e, a := tc.expectedPullPolicy, actualPullPolicy; e != a {
			t.Errorf("unexpected pull policy: expected %v, got %v", e, a)
		}
	}
}
