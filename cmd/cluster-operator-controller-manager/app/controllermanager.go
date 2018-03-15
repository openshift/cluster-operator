/*
Copyright 2014 The Kubernetes Authors.

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

// Package app implements a server that runs a set of active
// components.  This includes replication controllers, service endpoints and
// nodes.
//
package app

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	goruntime "runtime"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/apiserver/pkg/server/healthz"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/discovery"
	kubeinformers "k8s.io/client-go/informers"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/configz"

	"github.com/openshift/cluster-operator/cmd/cluster-operator-controller-manager/app/options"
	"github.com/openshift/cluster-operator/pkg/api"
	"github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clusteroperatorinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/controller/accept"
	"github.com/openshift/cluster-operator/pkg/controller/cluster"
	"github.com/openshift/cluster-operator/pkg/controller/components"
	"github.com/openshift/cluster-operator/pkg/controller/infra"
	"github.com/openshift/cluster-operator/pkg/controller/machine"
	"github.com/openshift/cluster-operator/pkg/controller/master-machineset"
	"github.com/openshift/cluster-operator/pkg/controller/compute-machineset"
	"github.com/openshift/cluster-operator/pkg/controller/master"
	"github.com/openshift/cluster-operator/pkg/version"
)

const (
	// ControllerStartJitter used when starting controller managers
	ControllerStartJitter = 1.0
)

// NewControllerManagerCommand creates a *cobra.Command object with default parameters
func NewControllerManagerCommand() *cobra.Command {
	s := options.NewCMServer()
	s.AddFlags(pflag.CommandLine, KnownControllers(), ControllersDisabledByDefault.List())
	cmd := &cobra.Command{
		Use: "cluster-operator-controller-manager",
		Long: `The OpenShift ClusterOperator controller manager is a daemon that embeds
the clusteroperator control loops. In applications of robotics and automation, a control
loop is a non-terminating loop that regulates the state of the system. In OpenShift,
a controller is a control loop that watches the shared state of the cluster through
the apiserver and makes changes attempting to move the current state towards the
desired state. Examples of controllers that ship with OpenShift ClusterOperator today are
the cluster controller, node group controller, and master node controller.`,
		Run: func(cmd *cobra.Command, args []string) {
		},
	}

	return cmd
}

// ResyncPeriod returns a function which generates a duration each time it is
// invoked; this is so that multiple controllers don't get into lock-step and all
// hammer the apiserver with list requests simultaneously.
func ResyncPeriod(s *options.CMServer) func() time.Duration {
	return func() time.Duration {
		factor := rand.Float64() + 1
		return time.Duration(float64(s.MinResyncPeriod.Nanoseconds()) * factor)
	}
}

// Run runs the CMServer.  This should never exit.
func Run(s *options.CMServer) error {
	// To help debugging, immediately log version
	glog.Infof("Version: %+v", version.Get())
	if err := s.Validate(KnownControllers(), ControllersDisabledByDefault.List()); err != nil {
		return err
	}

	log.SetOutput(os.Stdout)
	if lvl, err := log.ParseLevel(s.LogLevel); err != nil {
		log.Panic(err)
	} else {
		log.SetLevel(lvl)
	}

	if c, err := configz.New("componentconfig"); err == nil {
		c.Set(s.ControllerManagerConfiguration)
	} else {
		glog.Errorf("unable to register configz: %s", err)
	}

	kubeClient, leaderElectionClient, kubeconfig, err := createClients(s)
	if err != nil {
		return err
	}

	go startHTTP(s)

	recorder := createRecorder(kubeClient)

	run := func(stop <-chan struct{}) {
		clientBuilder := controller.SimpleClientBuilder{
			ClientConfig: kubeconfig,
		}
		ctx, err := CreateControllerContext(s, clientBuilder, stop)
		if err != nil {
			glog.Fatalf("error building controller context: %v", err)
		}

		if err := StartControllers(ctx, NewControllerInitializers()); err != nil {
			glog.Fatalf("error starting controllers: %v", err)
		}

		ctx.InformerFactory.Start(ctx.Stop)
		ctx.KubeInformerFactory.Start(ctx.Stop)
		close(ctx.InformersStarted)

		select {}
	}

	if !s.LeaderElection.LeaderElect {
		run(nil)
		panic("unreachable")
	}

	id, err := os.Hostname()
	if err != nil {
		return err
	}

	rl, err := resourcelock.New(s.LeaderElection.ResourceLock,
		s.LeaderElectionNamespace,
		"cluster-operator-controller-manager",
		leaderElectionClient.CoreV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorder,
		})
	if err != nil {
		glog.Fatalf("error creating lock: %v", err)
	}

	leaderelection.RunOrDie(leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: s.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: s.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   s.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				glog.Fatalf("leaderelection lost")
			},
		},
	})
	panic("unreachable")
}

func startHTTP(s *options.CMServer) {
	mux := http.NewServeMux()
	healthz.InstallHandler(mux)
	if s.EnableProfiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		if s.EnableContentionProfiling {
			goruntime.SetBlockProfileRate(1)
		}
	}
	configz.InstallHandler(mux)
	mux.Handle("/metrics", prometheus.Handler())

	server := &http.Server{
		Addr:    net.JoinHostPort(s.Address, strconv.Itoa(int(s.Port))),
		Handler: mux,
	}
	glog.Fatal(server.ListenAndServe())
}

func createRecorder(kubeClient *clientset.Clientset) record.EventRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})
	return eventBroadcaster.NewRecorder(api.Scheme, v1.EventSource{Component: "cluster-operator-controller-manager"})
}

func createClients(s *options.CMServer) (*clientset.Clientset, *clientset.Clientset, *restclient.Config, error) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags(s.K8sAPIServerURL, s.K8sKubeconfigPath)
	if err != nil {
		return nil, nil, nil, err
	}

	kubeconfig.ContentConfig.ContentType = s.ContentType
	// Override kubeconfig qps/burst settings from flags
	kubeconfig.QPS = s.KubeAPIQPS
	kubeconfig.Burst = int(s.KubeAPIBurst)
	kubeClient, err := clientset.NewForConfig(restclient.AddUserAgent(kubeconfig, "cluster-operator-controller-manager"))
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}
	leaderElectionClient := clientset.NewForConfigOrDie(restclient.AddUserAgent(kubeconfig, "leader-election"))
	return kubeClient, leaderElectionClient, kubeconfig, nil
}

// ControllerContext contains references to resources needed by the
// controllers.
type ControllerContext struct {
	// ClientBuilder will provide a client for this controller to use
	ClientBuilder controller.ClientBuilder

	// InformerFactory gives access to informers for the controller.
	InformerFactory clusteroperatorinformers.SharedInformerFactory

	// KubeInformerFactory gives access to kubernetes informers for the controller.
	KubeInformerFactory kubeinformers.SharedInformerFactory

	// Options provides access to init options for a given controller
	Options options.CMServer

	// AvailableResources is a map listing currently available resources
	AvailableResources map[schema.GroupVersionResource]bool

	// Stop is the stop channel
	Stop <-chan struct{}

	// InformersStarted is closed after all of the controllers have been initialized and are running.  After this point it is safe,
	// for an individual controller to start the shared informers. Before it is closed, they should not.
	InformersStarted chan struct{}
}

// IsControllerEnabled returns true is the controller with the specified name
// is enabled by default.
func (c ControllerContext) IsControllerEnabled(name string) bool {
	return IsControllerEnabled(name, ControllersDisabledByDefault, c.Options.Controllers...)
}

// IsControllerEnabled returns true is the controller with the specified name
// is enabled given the list of controllers and which are disabled by default.
func IsControllerEnabled(name string, disabledByDefaultControllers sets.String, controllers ...string) bool {
	hasStar := false
	for _, ctrl := range controllers {
		if ctrl == name {
			return true
		}
		if ctrl == "-"+name {
			return false
		}
		if ctrl == "*" {
			hasStar = true
		}
	}
	// if we get here, there was no explicit choice
	if !hasStar {
		// nothing on by default
		return false
	}
	if disabledByDefaultControllers.Has(name) {
		return false
	}

	return true
}

// InitFunc is used to launch a particular controller.  It may run additional "should I activate checks".
// Any error returned will cause the controller process to `Fatal`
// The bool indicates whether the controller was enabled.
type InitFunc func(ctx ControllerContext) (bool, error)

// KnownControllers returns the known controllers.
func KnownControllers() []string {
	return sets.StringKeySet(NewControllerInitializers()).List()
}

// ControllersDisabledByDefault are the names of the controllers that are
// disabled by default.
var ControllersDisabledByDefault = sets.NewString()

// NewControllerInitializers is a public map of named controller groups (you can start more than one in an init func)
// paired to their InitFunc.  This allows for structured downstream composition and subdivision.
func NewControllerInitializers() map[string]InitFunc {
	controllers := map[string]InitFunc{}
	controllers["cluster"] = startClusterController
	controllers["infra"] = startInfraController
	controllers["mastermachineset"] = startMasterMachineSetController
	controllers["computemachineset"] = startComputeMachineSetController
	controllers["machine"] = startMachineController
	controllers["master"] = startMasterController
	controllers["accept"] = startAcceptController
	controllers["components"] = startComponentsController
	return controllers
}

// GetAvailableResources gets the available resources registered in the API
// Server.
// TODO: In general, any controller checking this needs to be dynamic so
//  users don't have to restart their controller manager if they change the apiserver.
// Until we get there, the structure here needs to be exposed for the construction of a proper ControllerContext.
func GetAvailableResources(clientBuilder controller.ClientBuilder) (map[schema.GroupVersionResource]bool, error) {
	var (
		discoveryClient discovery.DiscoveryInterface
		allResources    map[schema.GroupVersionResource]bool
		healthzContent  string
	)

	// If apiserver is not running we should wait for some time and fail only then. This is particularly
	// important when we start apiserver and controller manager at the same time.
	err := wait.PollImmediate(10*time.Second, 3*time.Minute, func() (bool, error) {
		client, err := clientBuilder.Client("controller-discovery")
		if err != nil {
			glog.Errorf("Failed to get api versions from server: %v", err)
			return false, nil
		}

		healthStatus := 0
		resp := client.Discovery().RESTClient().Get().AbsPath("/healthz").Do().StatusCode(&healthStatus)
		if healthStatus != http.StatusOK {
			glog.Errorf("Server isn't healthy yet.  Waiting a little while.")
			return false, nil
		}
		content, _ := resp.Raw()
		healthzContent = string(content)

		discoveryClient = client.Discovery()

		resourceMap, err := discoveryClient.ServerResources()
		if err != nil {
			glog.Errorf("API Extensions have not all been registered yet. Waiting a little while. %v", err)
			return false, nil
		}

		clusterOperatorResourcesRegistered := false
		allResources = map[schema.GroupVersionResource]bool{}
		for _, apiResourceList := range resourceMap {
			version, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
			if err != nil {
				glog.Errorf("unable to parse group version %q: %v", apiResourceList.GroupVersion, err)
				return false, nil
			}
			if apiResourceList.GroupVersion == v1alpha1.SchemeGroupVersion.String() {
				clusterOperatorResourcesRegistered = true
			}
			for _, apiResource := range apiResourceList.APIResources {
				allResources[version.WithResource(apiResource.Name)] = true
			}
		}

		if !clusterOperatorResourcesRegistered {
			glog.Errorf("Cluster operator resources are not registered yet with the API server. Waiting a little while.")
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get api versions from server: %v: %v", healthzContent, err)
	}

	return allResources, nil
}

// CreateControllerContext creates a context struct containing references to resources needed by the
// controllers such as the clientBuilder.
func CreateControllerContext(s *options.CMServer, clientBuilder controller.ClientBuilder, stop <-chan struct{}) (ControllerContext, error) {
	versionedClient := clientBuilder.ClientOrDie("shared-informers")
	kubeClient := clientBuilder.KubeClientOrDie("shared-informers")
	sharedInformers := clusteroperatorinformers.NewSharedInformerFactory(versionedClient, ResyncPeriod(s)())
	kubeSharedInformers := kubeinformers.NewSharedInformerFactory(kubeClient, ResyncPeriod(s)())

	availableResources, err := GetAvailableResources(clientBuilder)
	if err != nil {
		return ControllerContext{}, err
	}

	ctx := ControllerContext{
		ClientBuilder:       clientBuilder,
		InformerFactory:     sharedInformers,
		KubeInformerFactory: kubeSharedInformers,
		Options:             *s,
		AvailableResources:  availableResources,
		Stop:                stop,
		InformersStarted:    make(chan struct{}),
	}
	return ctx, nil
}

// StartControllers starts all of the specified controllers.
func StartControllers(ctx ControllerContext, controllers map[string]InitFunc) error {
	for controllerName, initFn := range controllers {
		if !ctx.IsControllerEnabled(controllerName) {
			glog.Warningf("%q is disabled", controllerName)
			continue
		}

		time.Sleep(wait.Jitter(ctx.Options.ControllerStartInterval.Duration, ControllerStartJitter))

		glog.V(1).Infof("Starting %q", controllerName)
		started, err := initFn(ctx)
		if err != nil {
			glog.Errorf("Error starting %q", controllerName)
			return err
		}
		if !started {
			glog.Warningf("Skipping %q", controllerName)
			continue
		}
		glog.Infof("Started %q", controllerName)
	}

	return nil
}

func startClusterController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go cluster.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().Clusters(),
		ctx.InformerFactory.Clusteroperator().V1alpha1().MachineSets(),
		ctx.InformerFactory.Clusteroperator().V1alpha1().ClusterVersions(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-cluster-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-cluster-controller"),
	).Run(int(ctx.Options.ConcurrentClusterSyncs), ctx.Stop)
	return true, nil
}

func startInfraController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go infra.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().Clusters(),
		ctx.KubeInformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-infra-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-infra-controller"),
		ctx.Options.AnsibleImage,
		v1.PullPolicy(ctx.Options.AnsibleImagePullPolicy),
	).Run(int(ctx.Options.ConcurrentClusterSyncs), ctx.Stop)
	return true, nil
}

func startComputeMachineSetController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go computemachineset.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().MachineSets(),
		ctx.InformerFactory.Clusteroperator().V1alpha1().Machines(),
		////ctx.KubeInformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-compute-machine-set-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-compute-machine-set-controller"),
		////ctx.Options.AnsibleImage,
		// ^^ FIXME
		////v1.PullPolicy(ctx.Options.AnsibleImagePullPolicy),
		// ^^ FIXME
	).Run(int(ctx.Options.ConcurrentMachineSetSyncs), ctx.Stop)
	return true, nil
}

func startMasterMachineSetController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go mastermachineset.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().MachineSets(),
		ctx.KubeInformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-master-machine-set-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-master-machine-set-controller"),
		ctx.Options.AnsibleImage,
		v1.PullPolicy(ctx.Options.AnsibleImagePullPolicy),
	).Run(int(ctx.Options.ConcurrentMachineSetSyncs), ctx.Stop)
	return true, nil
}

func startMachineController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go machine.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().Machines(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-machine-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-machine-controller"),
	).Run(int(ctx.Options.ConcurrentMachineSyncs), ctx.Stop)
	return true, nil
}

func startMasterController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go master.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().MachineSets(),
		ctx.KubeInformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-master-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-master-controller"),
		ctx.Options.AnsibleImage,
		v1.PullPolicy(ctx.Options.AnsibleImagePullPolicy),
	).Run(int(ctx.Options.ConcurrentMasterSyncs), ctx.Stop)
	return true, nil
}

func startAcceptController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go accept.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().MachineSets(),
		ctx.KubeInformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-accept-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-accept-controller"),
		ctx.Options.AnsibleImage,
		v1.PullPolicy(ctx.Options.AnsibleImagePullPolicy),
	).Run(int(ctx.Options.ConcurrentAcceptSyncs), ctx.Stop)
	return true, nil
}

func startComponentsController(ctx ControllerContext) (bool, error) {
	if !resourcesAvailable(ctx) {
		return false, nil
	}
	go components.NewController(
		ctx.InformerFactory.Clusteroperator().V1alpha1().MachineSets(),
		ctx.KubeInformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.KubeClientOrDie("clusteroperator-components-controller"),
		ctx.ClientBuilder.ClientOrDie("clusteroperator-components-controller"),
		ctx.Options.AnsibleImage,
		v1.PullPolicy(ctx.Options.AnsibleImagePullPolicy),
	).Run(int(ctx.Options.ConcurrentComponentSyncs), ctx.Stop)
	return true, nil
}

func resourcesAvailable(ctx ControllerContext) bool {
	gvrs := []schema.GroupVersionResource{
		{Group: "clusteroperator.openshift.io", Version: "v1alpha1", Resource: "clusters"},
		{Group: "clusteroperator.openshift.io", Version: "v1alpha1", Resource: "machinesets"},
	}

	for _, gvr := range gvrs {
		if !ctx.AvailableResources[gvr] {
			return false
		}
	}

	return true
}
