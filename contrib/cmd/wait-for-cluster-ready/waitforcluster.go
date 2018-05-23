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

// Waits for a given cluster to complete installing

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/cluster-operator/pkg/controller"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

func main() {
	pflag.Parse()
	cmd := NewWaitForClusterCommand()
	err := cmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}

func NewWaitForClusterCommand() *cobra.Command {
	var namespace string
	pflag.StringVarP(&namespace, "namespace", "n", "", "namespace for cluster")
	return &cobra.Command{
		Use:   "wait-for-cluster CLUSTER-NAME",
		Short: "Wait for a cluster to finish installing",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.Usage()
				return nil
			}
			return waitForCluster(namespace, args[0])
		},
	}
}

func waitForCluster(clusterNamespace, clusterName string) error {
	capiClient, namespace, err := client()
	if err != nil {
		return fmt.Errorf("cannot obtain client: %v", err)
	}
	if len(clusterNamespace) == 0 {
		clusterNamespace = namespace
	}

	cluster, err := capiClient.ClusterV1alpha1().Clusters(clusterNamespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot retrieve cluster %s/%s: %v", namespace, clusterName, err)
	}

	return waitForClusterReady(capiClient, cluster)
}

func waitForClusterReady(capiClient *capiclient.Clientset, cluster *capiv1.Cluster) error {
	for {
		w, err := capiClient.ClusterV1alpha1().Clusters(cluster.Namespace).Watch(metav1.ListOptions{})
		if err != nil {
			return err
		}
		for e := range w.ResultChan() {
			c := e.Object.(*capiv1.Cluster)
			if c.Name == cluster.Name {
				status, err := controller.ClusterStatusFromClusterAPI(c)
				if err != nil {
					continue
				}
				if status.Ready {
					fmt.Printf("Cluster %s/%s is ready.\n", cluster.Namespace, cluster.Name)
					return nil
				}
			}
		}
	}
}

func client() (*capiclient.Clientset, string, error) {
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
	capiClient, err := capiclient.NewForConfig(cfg)
	if err != nil {
		return nil, "", err
	}
	return capiClient, namespace, nil
}
