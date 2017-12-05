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
// the state of boatswain API resources.

package componentconfig

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/staebler/boatswain/pkg/kubernetes/pkg/apis/componentconfig"
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

	// BoatswainAPIServerURL is the URL for the boatswain API
	// server.
	BoatswainAPIServerURL string
	// BoatswainKubeconfigPath is the path to the kubeconfig file with
	// information about the boatswain API server.
	BoatswainKubeconfigPath string
	// BoatswainInsecureSkipVerify controls whether a client verifies the
	// server's certificate chain and host name.
	// If BoatswainInsecureSkipVerify is true, TLS accepts any certificate
	// presented by the server and any host name in that certificate.
	// In this mode, TLS is susceptible to man-in-the-middle attacks.
	// This should be used only for testing.
	BoatswainInsecureSkipVerify bool

	// minResyncPeriod is the resync period in reflectors; will be random between
	// minResyncPeriod and 2*minResyncPeriod.
	MinResyncPeriod metav1.Duration

	// concurrentClusterSyncs is the number of cluster objects that are
	// allowed to sync concurrently. Larger number = more responsive clusters,
	// but more CPU (and network) load.
	ConcurrentClusterSyncs int32

	// concurrentNodeGroupSyncs is the number of node group objects that are
	// allowed to sync concurrently. Larger number = more responsive node groups,
	// but more CPU (and network) load.
	ConcurrentNodeGroupSyncs int32

	// concurrentNodeSyncs is the number of node objects that are
	// allowed to sync concurrently. Larger number = more responsive nodes,
	// but more CPU (and network) load.
	ConcurrentNodeSyncs int32

	// concurrentMasterNodeSyncs is the number of master node objects that are
	// allowed to sync concurrently. Larger number = more responsive master nodes,
	// but more CPU (and network) load.
	ConcurrentMasterNodeSyncs int32

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
