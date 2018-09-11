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

package dnszone

import (
	"testing"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func dnsZone() *coapi.DNSZone {
	return &coapi.DNSZone{
		ObjectMeta: metav1.ObjectMeta{},
	}
}

// TestClusterVersionStrategyTrivial is the testing of the trivial hardcoded
// boolean flags.
func TestDNSZoneStrategyTrivial(t *testing.T) {
	if !dnsZoneRESTStrategies.NamespaceScoped() {
		t.Errorf("cluster version create must be namespace scoped")
	}
	if dnsZoneRESTStrategies.AllowCreateOnUpdate() {
		t.Errorf("cluster version should not allow create on update")
	}
	if dnsZoneRESTStrategies.AllowUnconditionalUpdate() {
		t.Errorf("cluster version should not allow unconditional update")
	}
}

func TestDNSZoneCreate(t *testing.T) {
	// Create a cluster version
	hz := &coapi.DNSZone{}

	// Canonicalize the cluster
	dnsZoneRESTStrategies.PrepareForCreate(nil, hz)
}
