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
	"path"
	"testing"

	"github.com/stretchr/testify/assert"

	kapi "k8s.io/api/core/v1"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	testImageName = "test-image"
	testJobName   = "test-job"
	testInventory = "test-inventory"
	testVars      = "test-vars"
)

var (
	testHardware = &clusteroperator.ClusterHardwareSpec{}
)

func TestGeneratePlaybooksJob(t *testing.T) {
	cases := []struct {
		name      string
		playbooks []string
	}{
		{
			name: "no playbooks",
		},
		{
			name:      "single playbook",
			playbooks: []string{"single"},
		},
		{
			name:      "multiple playbooks",
			playbooks: []string{"first", "second", "third"},
		},
	}
	for _, tc := range cases {
		generator := NewJobGenerator()
		job, configmap := generator.GeneratePlaybooksJob(testJobName, testHardware, tc.playbooks, testInventory, testVars, "image-name", kapi.PullAlways)
		if assert.Equal(t, len(tc.playbooks), len(job.Spec.Template.Spec.Containers)) {
			for i, playbook := range tc.playbooks {
				assert.Equal(t, playbook, job.Spec.Template.Spec.Containers[i].Name)
				assert.Contains(t, job.Spec.Template.Spec.Containers[i].Env, kapi.EnvVar{
					Name:  "PLAYBOOK_FILE",
					Value: path.Join(openshiftAnsibleContainerDir, playbook),
				})
			}
		}
		assert.Equal(t, testInventory, configmap.Data["hosts"])
		assert.Equal(t, testVars, configmap.Data["vars"])
	}
}

func TestContainerNameForPlaybook(t *testing.T) {
	cases := []struct {
		name     string
		playbook string
		expected string
	}{
		{
			name:     "simple",
			playbook: "name",
			expected: "name",
		},
		{
			name:     "extension",
			playbook: "name.yml",
			expected: "name",
		},
		{
			name:     "directory",
			playbook: "path/to/name",
			expected: "name",
		},
		{
			name:     "directory and extension",
			playbook: "path/to/name.yml",
			expected: "name",
		},
		{
			name:     "replacements",
			playbook: "full_name",
			expected: "full-name",
		},
		{
			name:     "gamut of runes",
			playbook: "Full_Name@123&Main",
			expected: "Full-Name-123-Main",
		},
		{
			name:     "multiple periods",
			playbook: "full.name.yml",
			expected: "full-name",
		},
		{
			name:     "multiple periods",
			playbook: "full.name.yml",
			expected: "full-name",
		},
		{
			name:     "ending in period",
			playbook: "full.name.",
			expected: "full-name",
		},
		{
			name:     "everything",
			playbook: "path/to/Full_Name.123&Main.yml",
			expected: "Full-Name-123-Main",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := containerNameForPlaybook(tc.playbook)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
