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

package server

import (
	"fmt"
	"net"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	genericapiserver "k8s.io/apiserver/pkg/server"
	kubeinformers "k8s.io/client-go/informers"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/staebler/boatswain/cmd/boatswain-apiserver/app/options"
	"github.com/staebler/boatswain/pkg/api"
	boatswainadmission "github.com/staebler/boatswain/pkg/apiserver/admission"
	"github.com/staebler/boatswain/pkg/apiserver/authenticator"
	"github.com/staebler/boatswain/pkg/client/clientset_generated/internalclientset"
	informers "github.com/staebler/boatswain/pkg/client/informers_generated/internalversion"
	"github.com/staebler/boatswain/pkg/version"
)

// boatswainConfig is a placeholder for configuration
type boatswainConfig struct {
	// the shared informers that know how to speak back to this apiserver
	sharedInformers informers.SharedInformerFactory
	// the shared informers that know how to speak back to kube apiserver
	kubeSharedInformers kubeinformers.SharedInformerFactory
	// the configured loopback client for this apiserver
	client internalclientset.Interface
	// the configured client for kube apiserver
	kubeClient kubeclientset.Interface
}

// buildGenericConfig takes the server options and produces the genericapiserver.RecommendedConfig associated with it
func buildGenericConfig(s *options.BoatswainServerRunOptions) (*genericapiserver.RecommendedConfig, *boatswainConfig, error) {
	// server configuration options
	if err := s.SecureServing.MaybeDefaultWithSelfSignedCerts(s.GenericServerRunOptions.AdvertiseAddress.String(), nil /*alternateDNS*/, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, nil, err
	}
	genericConfig := genericapiserver.NewRecommendedConfig(api.Codecs)
	if err := s.GenericServerRunOptions.ApplyTo(&genericConfig.Config); err != nil {
		return nil, nil, err
	}
	if err := s.SecureServing.ApplyTo(&genericConfig.Config); err != nil {
		return nil, nil, err
	}
	if !s.DisableAuth {
		if err := s.Authentication.ApplyTo(&genericConfig.Config); err != nil {
			return nil, nil, err
		}
		if err := s.Authorization.ApplyTo(&genericConfig.Config); err != nil {
			return nil, nil, err
		}
	} else {
		// always warn when auth is disabled, since this should only be used for testing
		glog.Warning("Authentication and authorization disabled for testing purposes")
		genericConfig.Authenticator = &authenticator.AnyUserAuthenticator{}
		genericConfig.Authorizer = authorizerfactory.NewAlwaysAllowAuthorizer()
	}

	if err := s.Audit.ApplyTo(&genericConfig.Config); err != nil {
		return nil, nil, err
	}

	// TODO: add support for OpenAPI config
	// see https://github.com/kubernetes-incubator/service-catalog/issues/721
	genericConfig.SwaggerConfig = genericapiserver.DefaultSwaggerConfig()
	// TODO: investigate if we need metrics unique to boatswain, but take defaults for now
	// see https://github.com/kubernetes-incubator/service-catalog/issues/677
	genericConfig.EnableMetrics = true
	// TODO: add support to default these values in build
	// see https://github.com/kubernetes-incubator/service-catalog/issues/722
	serviceCatalogVersion := version.Get()
	genericConfig.Version = &serviceCatalogVersion

	// FUTURE: use protobuf for communication back to itself?
	client, err := internalclientset.NewForConfig(genericConfig.LoopbackClientConfig)
	if err != nil {
		glog.Errorf("Failed to create clientset for boatswain self-communication: %v", err)
		return nil, nil, err
	}
	sharedInformers := informers.NewSharedInformerFactory(client, 10*time.Minute)

	boatswainConfig := &boatswainConfig{
		client:          client,
		sharedInformers: sharedInformers,
	}

	inClusterConfig, err := restclient.InClusterConfig()
	if err != nil {
		glog.Errorf("Failed to get kube client config: %v", err)
		return nil, nil, err
	}
	inClusterConfig.GroupVersion = &schema.GroupVersion{}

	kubeClient, err := kubeclientset.NewForConfig(inClusterConfig)
	if err != nil {
		glog.Errorf("Failed to create clientset interface: %v", err)
		return nil, nil, err
	}

	kubeSharedInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
	genericConfig.SharedInformerFactory = kubeSharedInformers

	// TODO: we need upstream to package AlwaysAdmit, or stop defaulting to it!
	// NOTE: right now, we only run admission controllers when on kube cluster.
	genericConfig.AdmissionControl, err = buildAdmission(s, client, sharedInformers, kubeClient, kubeSharedInformers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize admission: %v", err)
	}

	boatswainConfig.kubeClient = kubeClient
	boatswainConfig.kubeSharedInformers = kubeSharedInformers

	return genericConfig, boatswainConfig, nil
}

// buildAdmission constructs the admission chain
func buildAdmission(s *options.BoatswainServerRunOptions,
	client internalclientset.Interface, sharedInformers informers.SharedInformerFactory,
	kubeClient kubeclientset.Interface, kubeSharedInformers kubeinformers.SharedInformerFactory) (admission.Interface, error) {

	admissionControlPluginNames := s.Admission.PluginNames
	glog.Infof("Admission control plugin names: %v", admissionControlPluginNames)
	var err error

	pluginInitializer := boatswainadmission.NewPluginInitializer(client, sharedInformers, kubeClient, kubeSharedInformers)
	admissionConfigProvider, err := admission.ReadAdmissionConfiguration(admissionControlPluginNames, s.Admission.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin config: %v", err)
	}
	return s.Admission.Plugins.NewFromPlugins(admissionControlPluginNames, admissionConfigProvider, pluginInitializer)
}

// addPostStartHooks adds the common post start hooks we invoke when using either server storage option.
func addPostStartHooks(server *genericapiserver.GenericAPIServer, scConfig *boatswainConfig, stopCh <-chan struct{}) {
	server.AddPostStartHook("start-boatswain-apiserver-informers", func(context genericapiserver.PostStartHookContext) error {
		glog.Infof("Starting shared informers")
		scConfig.sharedInformers.Start(stopCh)
		if scConfig.kubeSharedInformers != nil {
			scConfig.kubeSharedInformers.Start(stopCh)
		}
		glog.Infof("Started shared informers")
		return nil
	})
}
