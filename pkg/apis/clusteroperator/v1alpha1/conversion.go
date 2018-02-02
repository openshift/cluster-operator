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

package v1alpha1

import "fmt"

// These functions are used for field selectors. They are only needed if
// field selection is made available for types.

// ClusterFieldLabelConversionFunc does not convert anything, just returns
// what it's given for the supported fields, and errors for unsupported.
func ClusterFieldLabelConversionFunc(label, value string) (string, string, error) {
	switch label {
	case "spec.sampleField":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}

// MachineSetFieldLabelConversionFunc does not convert anything, just returns
// what it's given for the supported fields, and errors for unsupported.
func MachineSetFieldLabelConversionFunc(label, value string) (string, string, error) {
	switch label {
	case "spec.sampleField":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}

// MachineFieldLabelConversionFunc does not convert anything, just returns
// what it's given for the supported fields, and errors for unsupported.
func MachineFieldLabelConversionFunc(label, value string) (string, string, error) {
	switch label {
	case "spec.sampleField":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}
