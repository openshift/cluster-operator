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

package validation

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/staebler/boatswain/pkg/apis/boatswain"
)

func TestValidateHost(t *testing.T) {
	cases := []struct {
		name  string
		host  *boatswain.Host
		valid bool
	}{
		{
			name: "invalid host - host with namespace",
			host: &boatswain.Host{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "oops",
				},
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		errs := ValidateHost(tc.host)
		if len(errs) != 0 && tc.valid {
			t.Errorf("%v: unexpected error: %v", tc.name, errs)
			continue
		} else if len(errs) == 0 && !tc.valid {
			t.Errorf("%v: unexpected success", tc.name)
		}
	}
}
