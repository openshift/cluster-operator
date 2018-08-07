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

package cluster

// Waits for a given cluster to complete installing

import (
	"fmt"
	"os"
	"time"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	appsv1 "github.com/openshift/api/apps/v1"
	appsclient "github.com/openshift/client-go/apps/clientset/versioned"

	coapiv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclient "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	"github.com/openshift/cluster-operator/pkg/controller"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

var log *logger.Entry

type WaitForClusterOptions struct {
	Name                      string
	Namespace                 string
	ClusterResourceTimeout    time.Duration
	InfraProvisioningTimeout  time.Duration
	MasterInstallTimeout      time.Duration
	ClusterAPIInstallTimeout  time.Duration
	RemoteMachineSetsTimeout  time.Duration
	RemoteNodesTimeout        time.Duration
	RegistryDeploymentTimeout time.Duration
	RouterDeploymentTimeout   time.Duration
}

func NewWaitForClusterCommand() *cobra.Command {
	opt := &WaitForClusterOptions{}
	logLevel := "info"
	cmd := &cobra.Command{
		Use:   "wait-for-cluster-ready CLUSTERDEPLOYMENT-NAME",
		Short: "Wait for a cluster deployment to create a cluster and verify that the cluster is ready",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				cmd.Usage()
				return
			}

			// Set log level
			level, err := logger.ParseLevel(logLevel)
			if err != nil {
				logger.WithError(err).Error("Cannot parse log level")
				os.Exit(1)
			}

			log = logger.NewEntry(&logger.Logger{
				Out: os.Stdout,
				Formatter: &logger.TextFormatter{
					FullTimestamp: true,
				},
				Hooks: make(logger.LevelHooks),
				Level: level,
			})

			// Run command
			opt.Name = args[0]
			err = opt.WaitForCluster()
			if err != nil {
				log.Errorf("Error: %v", err)
				os.Exit(1)
			}
		},
	}
	flags := cmd.Flags()
	flags.StringVarP(&opt.Namespace, "namespace", "n", "", "namespace for cluster/cluster deployment")
	flags.StringVar(&logLevel, "loglevel", "info", "log level, one of: debug, info, warn, error, fatal, panic")
	flags.DurationVar(&opt.ClusterResourceTimeout, "cluster-resource-timeout", 5*time.Minute, "time to wait for initial cluster creation from clusterdeployment.")
	flags.DurationVar(&opt.InfraProvisioningTimeout, "infra-provisioning-timeout", 10*time.Minute, "time to wait for infra provisioning to complete.")
	flags.DurationVar(&opt.MasterInstallTimeout, "master-install-timeout", 25*time.Minute, "time to wait for master install to complete.")
	flags.DurationVar(&opt.ClusterAPIInstallTimeout, "cluster-api-install-timeout", 10*time.Minute, "time to wait for cluster API install to complete.")
	flags.DurationVar(&opt.RemoteMachineSetsTimeout, "remote-machinesets-timeout", 5*time.Minute, "time to wait for machinesets to be created in remote cluster.")
	flags.DurationVar(&opt.RemoteNodesTimeout, "remote-nodes-timeout", 15*time.Minute, "time to wait for remote nodes to become ready.")
	flags.DurationVar(&opt.RegistryDeploymentTimeout, "registry-deployment-timeout", 10*time.Minute, "time to wait for registry to deploy successfully in target cluster.")
	flags.DurationVar(&opt.RouterDeploymentTimeout, "router-deployment-timeout", 10*time.Minute, "time to wait for router to deploy successfully in target cluster.")
	return cmd
}

type localClientContext struct {
	Namespace        string
	KubeClient       clientset.Interface
	ClusterAPIClient capiclient.Interface
	ClusterOpClient  clustopclient.Interface
}

type remoteClientContext struct {
	KubeClient       clientset.Interface
	ClusterAPIClient capiclient.Interface
	AppsClient       appsclient.Interface
}

func (o *WaitForClusterOptions) WaitForCluster() error {
	ctx, err := o.getLocalClients()
	if err != nil {
		log.WithError(err).Error("Cannot obtain local cluster clients")
		return err
	}
	if len(o.Namespace) == 0 {
		o.Namespace = ctx.Namespace
	}

	clusterName, err := o.getClusterName(ctx.ClusterOpClient)
	if err != nil {
		return err
	}

	log = log.WithField("cluster", fmt.Sprintf("%s/%s", o.Namespace, clusterName))

	err = o.waitForClusterResource(ctx.ClusterAPIClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForInfrastructure(ctx.ClusterAPIClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForMasterInstall(ctx.ClusterAPIClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForClusterAPI(ctx.ClusterAPIClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForRemoteMachineSets(ctx.KubeClient, ctx.ClusterOpClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForRemoteNodes(ctx.KubeClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForRegistry(ctx.KubeClient, clusterName)
	if err != nil {
		return err
	}

	err = o.waitForRouter(ctx.KubeClient, clusterName)
	if err != nil {
		return err
	}
	return nil
}

// waitForClusterResource waits for a cluster resource with the given name to exist.
func (o *WaitForClusterOptions) waitForClusterResource(client capiclient.Interface, name string) error {
	log.Info("Waiting for cluster resource to be created")
	err := wait.PollImmediate(5*time.Second, o.ClusterResourceTimeout, func() (bool, error) {
		var err error
		_, err = client.ClusterV1alpha1().Clusters(o.Namespace).Get(name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			log.Debug("Cluster resource does not exist yet")
			return false, nil
		}
		if err != nil {
			log.WithError(err).Error("unexpected error retrieving cluster")
			return false, err
		}
		log.Debug("Cluster resource was found")
		return true, nil
	})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			log.WithField("timeout", o.ClusterResourceTimeout).Error("Timed out waiting for cluster to be created")
		} else {
			log.WithError(err).Error("Failed to get a cluster resource")
		}
		return err
	}
	log.Info("Cluster has been created")
	return nil
}

func (o *WaitForClusterOptions) waitForInfrastructure(client capiclient.Interface, name string) error {
	log.Info("Waiting for infrastructure provisioning for cluster")
	startedProvision := false
	return o.waitForClusterStatus(client, name, o.InfraProvisioningTimeout, func(status *coapiv1.ClusterProviderStatus) bool {
		if status.Provisioned {
			log.Info("Infrastructure provisioned for cluster")
			return true
		}
		provisioning := controller.FindClusterCondition(status.Conditions, coapiv1.ClusterInfraProvisioning)
		if provisioning != nil && !startedProvision && provisioning.Status == corev1.ConditionTrue {
			startedProvision = true
			log.WithField("started", provisioning.LastTransitionTime).Info("Infrastructure started provisioning")
		}
		return false
	})
}

func (o *WaitForClusterOptions) waitForMasterInstall(client capiclient.Interface, name string) error {
	log.Info("Waiting for master install on cluster")
	startedInstall := false
	return o.waitForClusterStatus(client, name, o.MasterInstallTimeout, func(status *coapiv1.ClusterProviderStatus) bool {
		if status.ControlPlaneInstalled {
			log.Info("Master installed")
			return true
		}
		installing := controller.FindClusterCondition(status.Conditions, coapiv1.ControlPlaneInstalling)
		if installing != nil && !startedInstall && installing.Status == corev1.ConditionTrue {
			startedInstall = true
			log.WithField("started", installing.LastTransitionTime).Info("Master started installing on cluster")
		}
		return false
	})
}

func (o *WaitForClusterOptions) waitForClusterAPI(client capiclient.Interface, name string) error {
	log.Info("Waiting for cluster API install")
	startedInstall := false
	return o.waitForClusterStatus(client, name, o.ClusterAPIInstallTimeout, func(status *coapiv1.ClusterProviderStatus) bool {
		if status.ClusterAPIInstalled {
			log.Info("Cluster API installed")
			return true
		}
		installing := controller.FindClusterCondition(status.Conditions, coapiv1.ClusterAPIInstalling)
		if installing != nil && !startedInstall && installing.Status == corev1.ConditionTrue {
			startedInstall = true
			log.WithField("started", installing.LastTransitionTime).Info("Cluster API started installing")
		}
		return false
	})
}

func (o *WaitForClusterOptions) waitForClusterStatus(client capiclient.Interface, name string, timeout time.Duration, checkStatus func(*coapiv1.ClusterProviderStatus) bool) error {
	timer := time.After(timeout)
	isReady := func(cluster *capiv1.Cluster) (bool, error) {
		status, err := controller.ClusterProviderStatusFromCluster(cluster)
		if err != nil {
			log.WithError(err).Error("Cannot obtain status from cluster")
			return false, err
		}
		if checkStatus(status) {
			return true, nil
		}
		return false, nil
	}
	log.Debug("Retrieving cluster")
	cluster, err := client.ClusterV1alpha1().Clusters(o.Namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).Error("Cannot retrieve cluster")
		return err
	}
	ready, err := isReady(cluster)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}

	for {
		w, err := client.ClusterV1alpha1().Clusters(o.Namespace).Watch(metav1.ListOptions{ResourceVersion: cluster.ResourceVersion})
		defer w.Stop()
		if err != nil {
			log.WithError(err).Error("Unable to start watch on clusters")
			return err
		}
		for {
			select {
			case e := <-w.ResultChan():
				if e.Type == watch.Modified {
					cluster := e.Object.(*capiv1.Cluster)
					if cluster.Name != name {
						continue
					}
					log.Debug("Received cluster modified event")
					ready, err := isReady(cluster)
					if err != nil {
						return err
					}
					if ready {
						return nil
					}
				}
			case <-timer:
				log.WithField("timeout", timeout).Error("Timed out waiting for cluster status")
				return wait.ErrWaitTimeout
			}
		}
	}
}

func (o *WaitForClusterOptions) waitForRemoteMachineSets(kubeClient clientset.Interface, clustopClient clustopclient.Interface, name string) error {
	ctx, err := o.getRemoteClients(kubeClient, name)
	if err != nil {
		log.WithError(err).Error("Cannot obtain remote client")
		return err
	}
	clusterDeployment, err := clustopClient.ClusteroperatorV1alpha1().ClusterDeployments(o.Namespace).Get(o.Name, metav1.GetOptions{})
	if err != nil {
		log.WithField("clusterdeployment", fmt.Sprintf("%s/%s", o.Namespace, o.Name)).WithError(err).
			Error("Cannot retrieve cluster deployment")
		return err
	}
	expectedRemoteMachineSetCount := len(clusterDeployment.Spec.MachineSets) - 1
	err = wait.PollImmediate(10*time.Second, o.RemoteMachineSetsTimeout, func() (bool, error) {
		log.Debug("Retrieving remote machinesets")
		list, err := ctx.ClusterAPIClient.ClusterV1alpha1().MachineSets("kube-cluster").List(metav1.ListOptions{})
		if err != nil {
			log.WithError(err).Error("Error retrieving remote machinesets")
			return false, nil
		}
		if len(list.Items) < expectedRemoteMachineSetCount {
			log.WithField("actual", len(list.Items)).WithField("expected", expectedRemoteMachineSetCount).
				Debug("Number of remote machinesets is less than expected")
			return false, nil
		}
		log.Info("Expected number of remote machinesets observed")
		return true, nil
	})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			log.WithField("timeout", o.RemoteMachineSetsTimeout).Error("Timed out waiting for remote machinesets to be created")
		} else {
			log.WithError(err).Error("Failed to get expected number of remote machinesets")
		}
		return err
	}
	return nil
}

func (o *WaitForClusterOptions) waitForRemoteNodes(kubeClient clientset.Interface, name string) error {
	log.Info("Waiting for remote nodes to appear in ready state")
	ctx, err := o.getRemoteClients(kubeClient, name)
	machineSets, err := ctx.ClusterAPIClient.ClusterV1alpha1().MachineSets("kube-cluster").List(metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("Cannot retrieve remote machinesets")
		return err
	}
	expectedNodes := 0
	for _, machineSet := range machineSets.Items {
		expectedNodes += int(*machineSet.Spec.Replicas)
	}
	log.Infof("Expecting to see %d non-master nodes", expectedNodes)
	err = wait.PollImmediate(10*time.Second, o.RemoteNodesTimeout, func() (bool, error) {
		log.Debug("Retrieving remote nodes")
		nodes, err := ctx.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			log.WithError(err).Error("Cannot retrieve remote nodes")
			return false, err
		}
		readyNodes, computeNodes := 0, 0
		for _, node := range nodes.Items {
			// Skip any master node
			if node.Labels != nil {
				if masterRole, isPresent := node.Labels["node-role.kubernetes.io/master"]; isPresent && masterRole == "true" {
					log.WithField("node", node.Name).Debug("Skipping node because it has the master label")
					continue
				}
			}
			log.WithField("node", node.Name).Debug("Found compute node")
			computeNodes++
			// Check if the node is ready
			isReady := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady {
					if condition.Status == corev1.ConditionTrue {
						isReady = true
					}
					break
				}
			}
			if isReady {
				log.WithField("node", node.Name).Debug("Node is ready")
				readyNodes++
			} else {
				log.WithField("node", node.Name).Debug("Node is not ready")
			}
		}
		log.Debugf("Found %d compute nodes, %d are ready", computeNodes, readyNodes)
		if readyNodes < expectedNodes {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			log.WithField("timeout", o.RemoteNodesTimeout).Error("Timed out waiting for nodes to become ready")
		} else {
			log.WithError(err).Error("Failed waiting for nodes to become ready")
		}
		return err
	}
	log.Info("Remote compute nodes are ready")
	return nil
}

func (o *WaitForClusterOptions) waitForRegistry(kubeClient clientset.Interface, name string) error {
	log.Info("Waiting for registry deployment on remote cluster")
	ctx, err := o.getRemoteClients(kubeClient, name)
	if err != nil {
		log.WithError(err).Error("Could not obtain remote client")
		return err
	}
	return o.waitForDeploymentConfigReady(ctx.AppsClient, o.RegistryDeploymentTimeout, "default", "docker-registry")
}

func (o *WaitForClusterOptions) waitForRouter(kubeClient clientset.Interface, name string) error {
	log.Info("Waiting for router deployment on remote cluster")
	ctx, err := o.getRemoteClients(kubeClient, name)
	if err != nil {
		log.WithError(err).Error("Could not obtain remote client")
		return err
	}
	return o.waitForDeploymentConfigReady(ctx.AppsClient, o.RouterDeploymentTimeout, "default", "router")
}

func (o *WaitForClusterOptions) waitForDeploymentConfigReady(client appsclient.Interface, timeOut time.Duration, namespace, name string) error {
	dcLog := log.WithField("deploymentconfig", fmt.Sprintf("%s/%s", namespace, name))
	err := wait.PollImmediate(20*time.Second, timeOut, func() (bool, error) {
		dcLog.Debug("Retrieving deployment config")
		dc, err := client.AppsV1().DeploymentConfigs(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			dcLog.WithError(err).Error("Cannot retrieve deployment config")
			return false, err
		}
		for _, condition := range dc.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		dcLog.Debug("Deployment available condition is not yet true")
		return false, nil
	})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			dcLog.WithField("timeout", timeOut).Error("Timed out waiting for deployment to become available")
		} else {
			dcLog.WithError(err).Error("Failed waiting for deployment to become available")
		}
		return err
	}
	dcLog.Info("Deployment config is available")
	return nil
}

func (o *WaitForClusterOptions) getClusterName(client clustopclient.Interface) (string, error) {
	cdLog := log.WithField("clusterdeployment", fmt.Sprintf("%s/%s", o.Namespace, o.Name))
	cdLog.Debug("Retrieving cluster deployment")
	clusterDeployment, err := client.ClusteroperatorV1alpha1().ClusterDeployments(o.Namespace).Get(o.Name, metav1.GetOptions{})
	if err != nil {
		cdLog.WithError(err).Error("Cannot retrieve cluster deployment")
		return "", err
	}
	if clusterDeployment.Spec.ClusterName == "" {
		cdLog.Error("cluster ID not set")
		return "", fmt.Errorf("cluster ID not set")
	}
	log.WithField("name", clusterDeployment.Spec.ClusterName).Debug("Obtained cluster name")
	return clusterDeployment.Spec.ClusterName, nil
}

func (o *WaitForClusterOptions) getLocalClients() (*localClientContext, error) {
	log.Debug("Obtaining clients for local cluster")
	result := &localClientContext{}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	cfg, err := kubeconfig.ClientConfig()
	if err != nil {
		log.WithError(err).Error("Cannot obtain client config")
		return nil, err
	}
	result.Namespace, _, err = kubeconfig.Namespace()
	if err != nil {
		log.WithError(err).Error("Cannot obtain default namespace from client config")
		return nil, err
	}
	result.ClusterAPIClient, err = capiclient.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create cluster API client from client config")
		return nil, err
	}
	result.ClusterOpClient, err = clustopclient.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create cluster operator client from client config")
		return nil, err
	}
	result.KubeClient, err = clientset.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("Cannot create kubernetes client from client config")
		return nil, err
	}
	log.Debug("Obtained clients for local cluster")
	return result, nil
}

func (o *WaitForClusterOptions) getRemoteClients(kubeClient clientset.Interface, name string) (*remoteClientContext, error) {
	log.Info("Obtaining clients for remote cluster")

	result := &remoteClientContext{}
	secretName := name + "-kubeconfig"
	secretLog := log.WithField("secret", fmt.Sprintf("%s/%s", o.Namespace, secretName))
	secret, err := kubeClient.CoreV1().Secrets(o.Namespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		secretLog.WithError(err).Error("Cannot retrieve kubeconfig secret")
		return nil, err
	}
	secretData := secret.Data["kubeconfig"]

	config, err := clientcmd.Load(secretData)
	if err != nil {
		secretLog.WithError(err).Error("Cannot create client config from secret data")
		return nil, err
	}

	kubeConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{})

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		log.WithError(err).Error("Cannot obtain client config")
		return nil, err
	}

	result.ClusterAPIClient, err = capiclient.NewForConfig(restConfig)
	if err != nil {
		log.WithError(err).Error("Cannot create cluster API client from client config")
		return nil, err
	}

	result.KubeClient, err = clientset.NewForConfig(restConfig)
	if err != nil {
		log.WithError(err).Error("Cannot create kubernetes client from client config")
		return nil, err
	}

	result.AppsClient, err = appsclient.NewForConfig(restConfig)
	if err != nil {
		log.WithError(err).Error("Cannot create OpenShift apps client from client config")
		return nil, err
	}
	log.Debug("Obtained clients for remote cluster")
	return result, nil
}
