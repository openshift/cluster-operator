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

package pretty

import (
	"testing"

	"github.com/staebler/boatswain/pkg/apis/boatswain/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHostName(t *testing.T) {
	e := `Host "namespace/name"`
	host := &v1alpha1.Host{
		ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: "namespace"},
	}
	g := HostName(host)
	if g != e {
		t.Fatalf("Unexpected value of PrettyName String; expected %v, got %v", e, g)
	}
}
