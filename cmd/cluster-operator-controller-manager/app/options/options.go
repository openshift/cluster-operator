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

// The controller is responsible for running control loops that reconcile
// the state of clusteroperator API resources with service brokers, service
// classes, service instances, and service instance credentials.

package options

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	k8scomponentconfig "github.com/openshift/cluster-operator/pkg/kubernetes/pkg/apis/componentconfig"
	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/client/leaderelectionconfig"

	"github.com/openshift/cluster-operator/pkg/apis/componentconfig"
)

// CMServer is the main context object for the controller manager.
type CMServer struct {
	componentconfig.ControllerManagerConfiguration
	LogLevel string
}

const (
	defaultContentType                   = "application/json"
	defaultBindAddress                   = "0.0.0.0"
	defaultPort                          = 10000
	defaultK8sKubeconfigPath             = "./kubeconfig"
	defaultClusterOperatorKubeconfigPath = "./clusteroperator-kubeconfig"
	defaultConcurrentSyncs               = 5
	defaultLeaderElectionNamespace       = "kube-system"
	defaultLogLevel                      = "info"
)

// NewCMServer creates a new CMServer with a default config.
func NewCMServer() *CMServer {
	s := CMServer{
		ControllerManagerConfiguration: componentconfig.ControllerManagerConfiguration{
			Controllers:                      []string{"*"},
			Address:                          defaultBindAddress,
			Port:                             defaultPort,
			ContentType:                      defaultContentType,
			K8sKubeconfigPath:                defaultK8sKubeconfigPath,
			ClusterOperatorKubeconfigPath:    defaultClusterOperatorKubeconfigPath,
			MinResyncPeriod:                  metav1.Duration{Duration: 12 * time.Hour},
			ConcurrentClusterSyncs:           defaultConcurrentSyncs,
			ConcurrentMasterSyncs:            defaultConcurrentSyncs,
			ConcurrentComponentSyncs:         defaultConcurrentSyncs,
			ConcurrentNodeConfigSyncs:        defaultConcurrentSyncs,
			ConcurrentDeployClusterAPISyncs:  defaultConcurrentSyncs,
			ConcurrentELBMachineSyncs:        defaultConcurrentSyncs,
			ConcurrentClusterDeploymentSyncs: defaultConcurrentSyncs,
			ConcurrentRemoteMachineSetSyncs:  defaultConcurrentSyncs,
			LeaderElection:                   leaderelectionconfig.DefaultLeaderElectionConfiguration(),
			LeaderElectionNamespace:          defaultLeaderElectionNamespace,
			ControllerStartInterval:          metav1.Duration{Duration: 0 * time.Second},
			EnableProfiling:                  true,
			EnableContentionProfiling:        false,
		},
	}
	s.LeaderElection.LeaderElect = true
	s.LogLevel = defaultLogLevel
	return &s
}

// AddFlags adds flags for a ControllerManagerServer to the specified FlagSet.
func (s *CMServer) AddFlags(fs *pflag.FlagSet, allControllers []string, disabledByDefaultControllers []string) {
	fs.StringSliceVar(&s.Controllers, "controllers", s.Controllers, fmt.Sprintf(""+
		"A list of controllers to enable.  '*' enables all on-by-default controllers, 'foo' enables the controller "+
		"named 'foo', '-foo' disables the controller named 'foo'.\nAll controllers: %s\nDisabled-by-default controllers: %s",
		strings.Join(allControllers, ", "), strings.Join(disabledByDefaultControllers, ", ")))
	fs.Var(k8scomponentconfig.IPVar{Val: &s.Address}, "address", "The IP address to serve on (set to 0.0.0.0 for all interfaces)")
	fs.Int32Var(&s.Port, "port", s.Port, "The port that the controller-manager's http service runs on")
	fs.StringVar(&s.ContentType, "api-content-type", s.ContentType, "Content type of requests sent to API servers")
	fs.StringVar(&s.K8sAPIServerURL, "k8s-api-server-url", "", "The URL for the k8s API server")
	fs.StringVar(&s.K8sKubeconfigPath, "k8s-kubeconfig", "", "Path to k8s core kubeconfig")
	fs.StringVar(&s.ClusterOperatorAPIServerURL, "clusteroperator-api-server-url", "", "The URL for the clusteroperator API server")
	fs.StringVar(&s.ClusterOperatorKubeconfigPath, "clusteroperator-kubeconfig", "", "Path to clusteroperator kubeconfig")
	fs.BoolVar(&s.ClusterOperatorInsecureSkipVerify, "clusteroperator-insecure-skip-verify", s.ClusterOperatorInsecureSkipVerify, "Skip verification of the TLS certificate for the clusteroperator API server")
	fs.DurationVar(&s.MinResyncPeriod.Duration, "min-resync-period", s.MinResyncPeriod.Duration, "The resync period in reflectors will be random between MinResyncPeriod and 2*MinResyncPeriod")
	fs.Int32Var(&s.ConcurrentClusterSyncs, "concurrent-cluster-syncs", s.ConcurrentClusterSyncs, "The number of cluster objects that are allowed to sync concurrently. Larger number = more responsive clusters, but more CPU (and network) load")
	fs.Int32Var(&s.ConcurrentMasterSyncs, "concurrent-master-syncs", s.ConcurrentMasterSyncs, "The number of master machine objects that are allowed to sync concurrently. Larger number = more responsive master machines, but more CPU (and network) load")
	fs.Int32Var(&s.ConcurrentComponentSyncs, "concurrent-component-syncs", s.ConcurrentComponentSyncs, "The number of master machine set objects that are allowed to install components concurrently. Larger number = more responsive accept jobs, but more CPU (and network) load")
	fs.Int32Var(&s.ConcurrentNodeConfigSyncs, "concurrent-nodeconfig-syncs", s.ConcurrentNodeConfigSyncs, "The number of clusters that are allowed to configure the node config daemonset concurrently. Larger number = more responsive node config jobs, but more CPU (and network) load")
	fs.Int32Var(&s.ConcurrentRemoteMachineSetSyncs, "concurrent-remotemachineset-syncs", s.ConcurrentRemoteMachineSetSyncs, "The number of machine sets we can sync to remote clusters concurrently. Larger number = more responsive sync jobs, but more CPU (and network ) load")
	fs.Int32Var(&s.ConcurrentDeployClusterAPISyncs, "concurrent-deploy-cluster-api-syncs", s.ConcurrentDeployClusterAPISyncs, "The number of master machine set objects that are allowed to install the upstream cluster API controllers concurrently. Larger number = more responsive accept jobs, but more CPU (and network) load")
	fs.Int32Var(&s.ConcurrentClusterDeploymentSyncs, "concurrent-cluster-deployment-syncs", s.ConcurrentClusterDeploymentSyncs, "The number of cluster deployment objects that are allowed to sync concurrently. Larger number = more responsive accept jobs, but more CPU (and network) load")
	fs.Int32Var(&s.ConcurrentELBMachineSyncs, "concurrent-elb-machine-syncs", s.ConcurrentELBMachineSyncs, "The number of master machine objects that are allowed to be added to the internal/external AWS ELB concurrently. Larger number = more responsive accept jobs, but more CPU (and network) load")
	fs.BoolVar(&s.EnableProfiling, "profiling", s.EnableProfiling, "Enable profiling via web interface host:port/debug/pprof/")
	fs.BoolVar(&s.EnableContentionProfiling, "contention-profiling", s.EnableContentionProfiling, "Enable lock contention profiling, if profiling is enabled")
	leaderelectionconfig.BindFlags(&s.LeaderElection, fs)
	fs.StringVar(&s.LeaderElectionNamespace, "leader-election-namespace", s.LeaderElectionNamespace, "Namespace to use for leader election lock")
	fs.StringVar(&s.LogLevel, "log-level", defaultLogLevel, "Log level (debug,info,warn,error,fatal)")
	fs.DurationVar(&s.ControllerStartInterval.Duration, "controller-start-interval", s.ControllerStartInterval.Duration, "Interval between starting controller managers.")

	utilfeature.DefaultFeatureGate.AddFlag(fs)
}

// Validate is used to validate the options and config before launching the controller manager
func (s *CMServer) Validate(allControllers []string, disabledByDefaultControllers []string) error {
	var errs []error

	allControllersSet := sets.NewString(allControllers...)
	for _, controller := range s.Controllers {
		if controller == "*" {
			continue
		}
		if strings.HasPrefix(controller, "-") {
			controller = controller[1:]
		}

		if !allControllersSet.Has(controller) {
			errs = append(errs, fmt.Errorf("%q is not in the list of known controllers", controller))
		}
	}

	return utilerrors.NewAggregate(errs)
}
