/*
Copyright 2016 The Kubernetes Authors.

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
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	dnsvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator"
)

// ValidateDNSZone validates a cluster version being created.
func ValidateDNSZone(dnsZone *clusteroperator.DNSZone) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateDNSZoneSpec(&dnsZone.Spec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateDNSZoneUpdate validates that a spec update of a DNSZone.
func ValidateDNSZoneUpdate(new *clusteroperator.DNSZone, old *clusteroperator.DNSZone) field.ErrorList {
	allErrs := field.ErrorList{}
	// For now updating cluster versions is not supported. In the future this may change if deemed useful.
	// In the meantime it will be necessary to create a new cluster version and trigger an upgrade if modifications
	// are required.

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(new.Spec, old.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateDNSZoneStatusUpdate validates an update to the status of a DNSZone.
func ValidateDNSZoneStatusUpdate(new *clusteroperator.DNSZone, old *clusteroperator.DNSZone) field.ErrorList {
	allErrs := field.ErrorList{}

	return allErrs
}

// ValidateDNSZoneSpec validates the spec of a DNSZone.
func ValidateDNSZoneSpec(spec *clusteroperator.DNSZoneSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// Make sure the DNS zone is specified.
	if spec.Zone == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("zone"), "must define zone"))
		return allErrs // If DNS zone is not specified, then no need to do further validation.
	}

	// Make sure that the DNS zone is actually valid.
	strErrs := dnsvalidation.IsDNS1123Subdomain(spec.Zone)
	for _, curStr := range strErrs {
		allErrs = append(allErrs, field.Invalid(field.NewPath("zone"), spec.Zone, curStr))
	}

	return allErrs
}
