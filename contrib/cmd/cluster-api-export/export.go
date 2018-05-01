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

package main

// Converts a given cluster-operator cluster to a cluster-api resource
// along with infra and compute machinesets

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	corev1scheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterv1scheme "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/scheme"

	coapi "github.com/openshift/cluster-operator/pkg/api"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
)

func main() {
	cmd := NewExportCommand()
	err := cmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}

func NewExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cluster-api-export CLUSTER-NAME",
		Short: "Export cluster API resources for a given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.Usage()
				return nil
			}
			return exportCluster(args[0], os.Stdout)
		},
	}
}

func exportCluster(clusterName string, out io.Writer) error {
	clusterOperatorClient, namespace, err := client()
	if err != nil {
		return fmt.Errorf("cannot obtain client: %v", err)
	}

	cluster, err := clusterOperatorClient.ClusteroperatorV1alpha1().Clusters(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot retrieve cluster %s/%s: %v", namespace, clusterName, err)
	}

	allMachineSets, err := clusterOperatorClient.ClusteroperatorV1alpha1().MachineSets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("cannot retrieve machine sets in %s: %v", namespace, err)
	}

	computeMachineSets := []*cov1.MachineSet{}
	for i, machineSet := range allMachineSets.Items {
		if machineSet.Spec.NodeType == cov1.NodeTypeCompute && metav1.IsControlledBy(&machineSet, cluster) {
			computeMachineSets = append(computeMachineSets, &allMachineSets.Items[i])
		}
	}

	result := &corev1.List{}
	capiCluster := &clusterv1.Cluster{}
	capiCluster.Name = cluster.Name
	capiCluster.Spec.ProviderConfig.Value = &runtime.RawExtension{
		Raw: []byte(serializeCOResource(cluster)),
	}
	result.Items = append(result.Items, runtime.RawExtension{
		Raw: []byte(serializeClusterAPIResource(capiCluster)),
	})

	for _, machineSet := range computeMachineSets {
		versionNamespace, versionName := machineSet.Spec.ClusterVersionRef.Namespace, machineSet.Spec.ClusterVersionRef.Name
		clusterVersion, err := clusterOperatorClient.ClusteroperatorV1alpha1().ClusterVersions(versionNamespace).Get(versionName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("cannot retrieve cluster version %s/%s: %v", versionNamespace, versionName, err)
		}
		if machineSet.Annotations == nil {
			machineSet.Annotations = map[string]string{}
		}
		machineSet.Annotations["cluster-operator.openshift.io/cluster-version"] = serializeCOResource(clusterVersion)

		capiMachineSet := &clusterv1.MachineSet{}
		capiMachineSet.Name = machineSet.Name
		replicas := int32(machineSet.Spec.Size)
		capiMachineSet.Spec.Replicas = &replicas
		capiMachineSet.Spec.Selector.MatchLabels = map[string]string{"machineset": machineSet.Name}

		machineTemplate := clusterv1.MachineTemplateSpec{}
		machineTemplate.Labels = map[string]string{"machineset": machineSet.Name}
		machineTemplate.Spec.Labels = map[string]string{"machineset": machineSet.Name}
		machineTemplate.Spec.ProviderConfig.Value = &runtime.RawExtension{
			Raw: []byte(serializeCOResource(machineSet)),
		}
		capiMachineSet.Spec.Template = machineTemplate
		result.Items = append(result.Items, runtime.RawExtension{
			Raw: []byte(serializeClusterAPIResource(capiMachineSet)),
		})
	}

	serialized := serializeCoreV1Resource(result)
	fmt.Fprintf(out, "%s", serialized)
	return nil
}

func serializeCOResource(object runtime.Object) string {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, coapi.Scheme, coapi.Scheme, false)
	encoder := coapi.Codecs.EncoderForVersion(serializer, cov1.SchemeGroupVersion)
	buffer := &bytes.Buffer{}
	encoder.Encode(object, buffer)
	return buffer.String()
}

func serializeClusterAPIResource(object runtime.Object) string {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, clusterv1scheme.Scheme, clusterv1scheme.Scheme, false)
	encoder := clusterv1scheme.Codecs.EncoderForVersion(serializer, clusterv1.SchemeGroupVersion)
	buffer := &bytes.Buffer{}
	encoder.Encode(object, buffer)
	return buffer.String()
}

func serializeCoreV1Resource(object runtime.Object) string {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, corev1scheme.Scheme, corev1scheme.Scheme, true)
	encoder := corev1scheme.Codecs.EncoderForVersion(serializer, corev1.SchemeGroupVersion)
	buffer := &bytes.Buffer{}
	encoder.Encode(object, buffer)
	return buffer.String()
}

func client() (*coclient.Clientset, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	cfg, err := kubeconfig.ClientConfig()
	if err != nil {
		return nil, "", err
	}
	namespace, _, err := kubeconfig.Namespace()
	if err != nil {
		return nil, "", err
	}
	coClient, err := coclient.NewForConfig(cfg)
	if err != nil {
		return nil, "", err
	}
	return coClient, namespace, nil
}
