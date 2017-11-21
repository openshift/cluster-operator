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

package host

import (
	"testing"

	boatswain "github.com/staebler/boatswain/pkg/apis/boatswain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func hostWithOldSpec() *boatswain.Host {
	return &boatswain.Host{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
		Spec: boatswain.HostSpec{
			URL: "https://kubernetes.default.svc:443/hosts/template.k8s.io",
		},
		Status: boatswain.HostStatus{
			Conditions: []sc.ServiceBrokerCondition{
				{
					Type:   sc.ServiceBrokerConditionReady,
					Status: sc.ConditionFalse,
				},
			},
		},
	}
}

func hostWithNewSpec() *boatswain.Host {
	b := hostWithOldSpec()
	b.Spec.URL = "new"
	return b
}

// TestHostStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestHostStrategyTrivial(t *testing.T) {
	if hostRESTStrategies.NamespaceScoped() {
		t.Errorf("host create must not be namespace scoped")
	}
	if hostRESTStrategies.NamespaceScoped() {
		t.Errorf("host update must not be namespace scoped")
	}
	if hostRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("host should not allow create on update")
	}
	if hostRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("host should not allow unconditional update")
	}
}

// TestHostCreate
func TestHostCreate(t *testing.T) {
	// Create a host or hosts
	host := &boatswain.Host{
		Spec: boatswain.HostSpec{
			URL: "abcd",
		},
		Status: boatswain.HostStatus{
			Conditions: nil,
		},
	}

	// Canonicalize the host
	hostRESTStrategies.PrepareForCreate(nil, host)

	if host.Status.Conditions == nil {
		t.Fatalf("Fresh host should have empty status")
	}
	if len(host.Status.Conditions) != 0 {
		t.Fatalf("Fresh host should have empty status")
	}
}

// TestHostUpdate tests that generation is incremented correctly when the
// spec of a Host is updated.
func TestHostUpdate(t *testing.T) {
	cases := []struct {
		name                      string
		older                     *boatswain.Host
		newer                     *boatswain.Host
		shouldGenerationIncrement bool
	}{
		{
			name:  "no spec change",
			older: hostWithOldSpec(),
			newer: hostWithOldSpec(),
			shouldGenerationIncrement: false,
		},
		{
			name:  "spec change",
			older: hostWithOldSpec(),
			newer: hostWithNewSpec(),
			shouldGenerationIncrement: true,
		},
	}

	for i := range cases {
		hostRESTStrategies.PrepareForUpdate(nil, cases[i].newer, cases[i].older)

		if cases[i].shouldGenerationIncrement {
			if e, a := cases[i].older.Generation+1, cases[i].newer.Generation; e != a {
				t.Fatalf("%v: expected %v, got %v for generation", cases[i].name, e, a)
			}
		} else {
			if e, a := cases[i].older.Generation, cases[i].newer.Generation; e != a {
				t.Fatalf("%v: expected %v, got %v for generation", cases[i].name, e, a)
			}
		}
	}
}

// TestHostUpdateForRelistRequests tests that the RelistRequests field is
// ignored during updates when it is the default value.
func TestHostUpdateForRelistRequests(t *testing.T) {
	cases := []struct {
		name          string
		oldValue      int64
		newValue      int64
		expectedValue int64
	}{
		{
			name:          "both default",
			oldValue:      0,
			newValue:      0,
			expectedValue: 0,
		},
		{
			name:          "old default",
			oldValue:      0,
			newValue:      1,
			expectedValue: 1,
		},
		{
			name:          "new default",
			oldValue:      1,
			newValue:      0,
			expectedValue: 1,
		},
		{
			name:          "neither default",
			oldValue:      1,
			newValue:      2,
			expectedValue: 2,
		},
	}
	for _, tc := range cases {
		oldBroker := hostWithOldSpec()
		oldBroker.Spec.RelistRequests = tc.oldValue

		newBroker := hostWithOldSpec()
		newBroker.Spec.RelistRequests = tc.newValue

		hostRESTStrategies.PrepareForUpdate(nil, newBroker, oldBroker)

		if e, a := tc.expectedValue, newBroker.Spec.RelistRequests; e != a {
			t.Errorf("%s: got unexpected RelistRequests: expected %v, got %v", tc.name, e, a)
		}
	}
}
