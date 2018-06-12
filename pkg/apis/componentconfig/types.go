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

// The controller is responsible for running control loops that reconcile
// the state of clusteroperator API resources.

package componentconfig

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/apis/componentconfig"
)

// ControllerManagerConfiguration encapsulates configuration for the
// controller manager.
type ControllerManagerConfiguration struct {
	metav1.TypeMeta

	// Controllers is the list of controllers to enable or disable
	// '*' means "all enabled by default controllers"
	// 'foo' means "enable 'foo'"
	// '-foo' means "disable 'foo'"
	// first item for a particular name wins
	Controllers []string

	// Address is the IP address to serve on (set to 0.0.0.0 for all interfaces).
	Address string
	// Port is the port that the controller's http service runs on.
	Port int32

	// ContentType is the content type for requests sent to API servers.
	ContentType string

	// kubeAPIQPS is the QPS to use while talking with kubernetes apiserver.
	KubeAPIQPS float32
	// kubeAPIBurst is the burst to use while talking with kubernetes apiserver.
	KubeAPIBurst int32

	// K8sAPIServerURL is the URL for the k8s API server.
	K8sAPIServerURL string
	// K8sKubeconfigPath is the path to the kubeconfig file with authorization
	// information.
	K8sKubeconfigPath string

	// ClusterOperatorAPIServerURL is the URL for the clusteroperator API
	// server.
	ClusterOperatorAPIServerURL string
	// ClusterOperatorKubeconfigPath is the path to the kubeconfig file with
	// information about the clusteroperator API server.
	ClusterOperatorKubeconfigPath string
	// ClusterOperatorInsecureSkipVerify controls whether a client verifies the
	// server's certificate chain and host name.
	// If ClusterOperatorInsecureSkipVerify is true, TLS accepts any certificate
	// presented by the server and any host name in that certificate.
	// In this mode, TLS is susceptible to man-in-the-middle attacks.
	// This should be used only for testing.
	ClusterOperatorInsecureSkipVerify bool

	// minResyncPeriod is the resync period in reflectors; will be random between
	// minResyncPeriod and 2*minResyncPeriod.
	MinResyncPeriod metav1.Duration

	// concurrentClusterSyncs is the number of cluster objects that are
	// allowed to sync concurrently. Larger number = more responsive clusters,
	// but more CPU (and network) load.
	ConcurrentClusterSyncs int32

	// concurrentMasterSyncs is the number of master machine objects that are
	// allowed to sync concurrently. Larger number = more responsive master machines,
	// but more CPU (and network) load.
	ConcurrentMasterSyncs int32

	// concurrentComponentSyncs is the number of master machine set objects that are
	// allowed to install components concurrently. Larger number = more responsive processing
	// but more CPU (and network) load.
	ConcurrentComponentSyncs int32

	// ConcurrentNodeConfigSyncs is the number of clusters that are allowed to be configuring
	// the node config daemonset concurrently. Larger number = more responsive processing
	// but more CPU (and network) load.
	ConcurrentNodeConfigSyncs int32

	// ConcurrentRemoteMachineSetSyncs is the number of machinesets we can be syncing out to remote clusters
	// concurrently. Larger number = more responsive processing but more CPU (and network) load.
	ConcurrentRemoteMachineSetSyncs int32

	// ConcurrentDeployClusterAPISyncs is the number of clusters that are allowed to be deploying
	// the upstream cluster API controllers concurrently. Larger number = more responsive processing
	// but more CPU (and network) load.
	ConcurrentDeployClusterAPISyncs int32

	// ConcurrentClusterDeploymentSyncs is the number of cluster deploymentss that are allowed to
	// sync concurrently. Larger number = more responsive processing but more CPU (and network) load.
	ConcurrentClusterDeploymentSyncs int32

	// ConcurrentELBMachineSyncs is the number of master machine objects that are
	// allowed to sync concurrently in the controller which adds them to the internal/external master ELBs.
	// Larger number = more responsive master machines, but more CPU (and network) load.
	ConcurrentELBMachineSyncs int32

	// leaderElection defines the configuration of leader election client.
	LeaderElection componentconfig.LeaderElectionConfiguration

	// LeaderElectionNamespace is the namespace to use for the leader election
	// lock.
	LeaderElectionNamespace string

	// How long to wait between starting controller managers
	ControllerStartInterval metav1.Duration

	// enableProfiling enables profiling via web interface host:port/debug/pprof/
	EnableProfiling bool

	// enableContentionProfiling enables lock contention profiling, if enableProfiling is true.
	EnableContentionProfiling bool
}
