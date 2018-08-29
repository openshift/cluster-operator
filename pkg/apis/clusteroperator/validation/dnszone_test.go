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
	"github.com/stretchr/testify/assert"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

var (
	validZone = &clusteroperator.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name: "valid-dns-zone",
		},
		Spec: clusteroperator.DNSZoneSpec{
			Zone: "valid.aws.example.com",
		},
	}

	invalidZoneEmptyString = &clusteroperator.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name: "invalid-dns-zone-empty-string",
		},
		Spec: clusteroperator.DNSZoneSpec{
			Zone: "",
		},
	}

	invalidZoneChars = &clusteroperator.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name: "invalid-dns-zone-chars",
		},
		Spec: clusteroperator.DNSZoneSpec{
			Zone: "%.example.com",
		},
	}

	invalidZoneLength = &clusteroperator.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name: "invalid-dns-zone-length",
		},
		Spec: clusteroperator.DNSZoneSpec{
			Zone: "this.is.a.very.long.dns.name.that.makes.it.invalid.zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz.example.com",
		},
	}
)

// TestValidateDNSZone tests the ValidateDNSZone function.
func TestValidateDNSZone(t *testing.T) {
	cases := []struct {
		name           string
		dnszone        *clusteroperator.DNSZone
		errorAssertion func(assert.TestingT, interface{}, ...interface{}) bool
	}{
		{
			name:           "valid zone name",
			dnszone:        validZone.DeepCopy(),
			errorAssertion: assert.Empty,
		},
		{
			name:           "invalid zone name - empty string",
			dnszone:        invalidZoneEmptyString.DeepCopy(),
			errorAssertion: assert.NotEmpty,
		},
		{
			name:           "invalid zone name - characters",
			dnszone:        invalidZoneChars.DeepCopy(),
			errorAssertion: assert.NotEmpty,
		},
		{
			name:           "invalid zone name - length",
			dnszone:        invalidZoneLength.DeepCopy(),
			errorAssertion: assert.NotEmpty,
		},
	}

	for _, tc := range cases {
		// arrange

		// act
		errs := ValidateDNSZone(tc.dnszone)

		// assert
		tc.errorAssertion(t, errs)
	}
}

// TestValidateDNSZoneUpdate tests the ValidateDNSZoneUpdate function. Updates are not
// supported at this time, so any modification should return an error.
func TestValidateDNSZoneUpdate(t *testing.T) {
	cases := []struct {
		name           string
		old            *clusteroperator.DNSZone
		new            *clusteroperator.DNSZone
		errorAssertion func(assert.TestingT, interface{}, ...interface{}) bool
	}{
		{
			name:           "valid",
			old:            validZone.DeepCopy(),
			new:            validZone.DeepCopy(),
			errorAssertion: assert.Empty,
		},
		{
			name: "modified zone name",
			old:  validZone.DeepCopy(),
			new: func() *clusteroperator.DNSZone {
				c := validZone.DeepCopy()
				c.Spec.Zone = "some.different.dns.zone.com"
				return c
			}(),
			errorAssertion: assert.NotEmpty,
		},
	}

	for _, tc := range cases {
		// Arrange

		// Act
		errs := ValidateDNSZoneUpdate(tc.new, tc.old)

		// Assert
		tc.errorAssertion(t, errs)
	}
}
