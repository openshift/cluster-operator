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

package testing

import (
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	boatswain "github.com/staebler/boatswain/pkg/apis/boatswain/v1alpha1"
	clientset "github.com/staebler/boatswain/pkg/client/clientset_generated/clientset/typed/boatswain/v1alpha1"
)

func TestRun(t *testing.T) {
	if _, found := os.LookupEnv("KUBERNETES_SERVICE_HOST"); !found {
		t.Skip("KUBERNETES_SERVICE_HOST not found in environment. Skipping because kube is not running.")
	}
	if _, found := os.LookupEnv("KUBERNETES_SERVICE_PORT"); !found {
		t.Skip("KUBERNETES_SERVICE_PORT not found in environment. Skipping because kube is not running.")
	}

	config, tearDown := StartTestServerOrDie(t)
	defer tearDown()

	client, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// test whether the server is really healthy after /healthz told us so
	t.Logf("Creating Host directly after being healthy")
	_, err = client.Hosts().Create(&boatswain.Host{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Host",
			APIVersion: "boatswain.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "host1",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create host: %v", err)
	}
}
