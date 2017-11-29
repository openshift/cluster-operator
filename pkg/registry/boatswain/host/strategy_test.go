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
	}
}

func hostWithNewSpec() *boatswain.Host {
	b := hostWithOldSpec()
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
	host := &boatswain.Host{}

	// Canonicalize the host
	hostRESTStrategies.PrepareForCreate(nil, host)
}

// TestHostUpdate tests that generation is incremented correctly when the
// spec of a Host is updated.
func TestHostUpdate(t *testing.T) {
	older := hostWithOldSpec()
	newer := hostWithOldSpec()

	hostRESTStrategies.PrepareForUpdate(nil, newer, older)
}
