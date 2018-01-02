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

package options

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/util/diff"
	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	auditwebhook "k8s.io/apiserver/plugin/pkg/audit/webhook"
)

func TestAddFlags(t *testing.T) {
	f := pflag.NewFlagSet("addflagstest", pflag.ContinueOnError)
	s := NewServerRunOptions()
	s.AddFlags(f)

	args := []string{
		"--admission-control=AlwaysDeny",
		"--admission-control-config-file=/admission-control-config",
		"--advertise-address=192.168.10.10",
		"--apiserver-count=5",
		"--audit-log-maxage=11",
		"--audit-log-maxbackup=12",
		"--audit-log-maxsize=13",
		"--audit-log-path=/var/log",
		"--audit-policy-file=/policy",
		"--audit-webhook-config-file=/webhook-config",
		"--audit-webhook-mode=blocking",
		"--audit-webhook-batch-buffer-size=42",
		"--audit-webhook-batch-max-size=43",
		"--audit-webhook-batch-max-wait=1s",
		"--audit-webhook-batch-throttle-qps=43.5",
		"--audit-webhook-batch-throttle-burst=44",
		"--audit-webhook-batch-initial-backoff=2s",
		"--authentication-token-webhook-cache-ttl=3m",
		"--authorization-webhook-cache-authorized-ttl=3m",
		"--authorization-webhook-cache-unauthorized-ttl=1m",
		"--bind-address=192.168.10.20",
		"--client-ca-file=/client-ca",
		"--cors-allowed-origins=10.10.10.100,10.10.10.200",
		"--contention-profiling=true",
		"--enable-logs-handler=false",
		"--enable-swagger-ui=true",
		"--etcd-quorum-read=false",
		"--etcd-keyfile=/var/run/kubernetes/etcd.key",
		"--etcd-certfile=/var/run/kubernetes/etcdce.crt",
		"--etcd-cafile=/var/run/kubernetes/etcdca.crt",
		"--request-timeout=2m",
		"--storage-backend=etcd2",
	}
	f.Parse(args)

	// This is a snapshot of expected options parsed by args.
	expected := &ClusterOperatorServerRunOptions{
		MasterCount: 5,
		GenericServerRunOptions: &apiserveroptions.ServerRunOptions{
			AdvertiseAddress:            net.ParseIP("192.168.10.10"),
			CorsAllowedOriginList:       []string{"10.10.10.100", "10.10.10.200"},
			MaxRequestsInFlight:         400,
			MaxMutatingRequestsInFlight: 200,
			RequestTimeout:              time.Duration(2) * time.Minute,
			MinRequestTimeout:           1800,
		},
		Admission: &apiserveroptions.AdmissionOptions{
			RecommendedPluginOrder: []string{"NamespaceLifecycle", "Initializers", "MutatingAdmissionWebhook", "ValidatingAdmissionWebhook"},
			DefaultOffPlugins:      []string{"Initializers", "MutatingAdmissionWebhook", "ValidatingAdmissionWebhook"},
			PluginNames:            []string{"AlwaysDeny"},
			ConfigFile:             "/admission-control-config",
			Plugins:                s.Admission.Plugins,
		},
		Etcd: &apiserveroptions.EtcdOptions{
			StorageConfig: storagebackend.Config{
				Type:       "etcd2",
				ServerList: nil,
				Prefix:     "/clusteroperator",
				DeserializationCacheSize: 0,
				Quorum:             false,
				KeyFile:            "/var/run/kubernetes/etcd.key",
				CAFile:             "/var/run/kubernetes/etcdca.crt",
				CertFile:           "/var/run/kubernetes/etcdce.crt",
				CompactionInterval: storagebackend.DefaultCompactInterval,
			},
			DefaultStorageMediaType: "application/json",
			DeleteCollectionWorkers: 1,
			EnableGarbageCollection: true,
			EnableWatchCache:        true,
			DefaultWatchCacheSize:   100,
		},
		SecureServing: &apiserveroptions.SecureServingOptions{
			BindAddress: net.ParseIP("192.168.10.20"),
			BindPort:    443,
			ServerCert: apiserveroptions.GeneratableKeyCert{
				CertDirectory: "/var/run/openshift-cluster-operator",
				PairName:      "apiserver",
			},
		},
		//		InsecureServing: &kubeoptions.InsecureServingOptions{
		//			BindAddress: net.ParseIP("127.0.0.1"),
		//			BindPort:    8080,
		//		},
		Audit: &apiserveroptions.AuditOptions{
			LogOptions: apiserveroptions.AuditLogOptions{
				Path:       "/var/log",
				MaxAge:     11,
				MaxBackups: 12,
				MaxSize:    13,
				Format:     "json",
			},
			WebhookOptions: apiserveroptions.AuditWebhookOptions{
				Mode:       "blocking",
				ConfigFile: "/webhook-config",
				BatchConfig: auditwebhook.BatchBackendConfig{
					BufferSize:     42,
					MaxBatchSize:   43,
					MaxBatchWait:   1 * time.Second,
					ThrottleQPS:    43.5,
					ThrottleBurst:  44,
					InitialBackoff: 2 * time.Second,
				},
			},
			PolicyFile: "/policy",
		},
		Features: &apiserveroptions.FeatureOptions{
			EnableSwaggerUI:           true,
			EnableProfiling:           true,
			EnableContentionProfiling: true,
		},
		Authentication: &genericoptions.DelegatingAuthenticationOptions{
			CacheTTL: 3 * time.Minute,
			ClientCert: genericoptions.ClientCertAuthenticationOptions{
				ClientCA: "/client-ca",
			},
			RequestHeader: genericoptions.RequestHeaderAuthenticationOptions{
				UsernameHeaders:     []string{"x-remote-user"},
				GroupHeaders:        []string{"x-remote-group"},
				ExtraHeaderPrefixes: []string{"x-remote-extra-"},
			},
		},
		Authorization: &genericoptions.DelegatingAuthorizationOptions{
			AllowCacheTTL: 3 * time.Minute,
			DenyCacheTTL:  1 * time.Minute,
		},
		EnableLogsHandler: false,
	}

	if !reflect.DeepEqual(expected, s) {
		t.Errorf("Got different run options than expected.\nDifference detected on:\n%s", diff.ObjectReflectDiff(expected, s))
	}
}
