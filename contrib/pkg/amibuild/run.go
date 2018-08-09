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

package amibuild

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func (g *BuildAMIOptions) RunPod(client clientset.Interface, pod *corev1.Pod, configMap *corev1.ConfigMap) error {
	_, err := client.CoreV1().ConfigMaps(g.Namespace).Create(configMap)
	if err != nil {
		return fmt.Errorf("error creating configmap %s/%s: %v", g.Namespace, configMap.Name, err)
	}

	actualPod, err := client.CoreV1().Pods(g.Namespace).Create(pod)
	if err != nil {
		return fmt.Errorf("error creating pod %s/%s: %v", g.Namespace, pod.Name, err)
	}

	watch, err := client.CoreV1().Pods(g.Namespace).Watch(metav1.ListOptions{
		FieldSelector:   fmt.Sprintf("metadata.name=%s", actualPod.Name),
		ResourceVersion: actualPod.ResourceVersion,
	})
	if err != nil {
		return fmt.Errorf("unable to start watch on pod %s/%s: %v", g.Namespace, actualPod.Name, err)
	}

	g.Logger.WithField("pod", fmt.Sprintf("%s/%s", actualPod.Namespace, actualPod.Name)).Infof("Waiting for pod to complete")

	for e := range watch.ResultChan() {
		p, ok := e.Object.(*corev1.Pod)
		if !ok {
			return fmt.Errorf("received event for unexpected object: %#v", e.Object)
		}
		switch p.Status.Phase {
		case corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			return fmt.Errorf("playbook execution failed on %s/%s", p.Namespace, p.Name)
		case corev1.PodUnknown:
			return fmt.Errorf("pod status has changed to unknown on %s/%s", p.Namespace, p.Name)
		}
	}
	return nil
}
