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

package deploy

import (
	"fmt"
	"os"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/cluster-api/cloud/vsphere"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/util"
	apiutil "sigs.k8s.io/cluster-api/util"
)

type deployer struct {
	token               string
	configPath          string
	machineDeployer     machineDeployer
	client              v1alpha1.ClusterV1alpha1Interface
	clientSet           clientset.Interface
	kubernetesClientSet kubernetes.Clientset
}

// NewDeployer returns a cloud provider specific deployer and
// sets kubeconfig path for the cluster to be deployed
func NewDeployer(configPath, namedMachinesPath string) *deployer {
	token := util.RandomToken()
	if configPath == "" {
		configPath = os.Getenv("KUBECONFIG")
		if configPath == "" {
			configPath = apiutil.GetDefaultKubeConfigPath()
		}
	} else {
		// This is needed for kubectl commands run later to create secret in function
		// CreateMachineControllerServiceAccount
		if err := os.Setenv("KUBECONFIG", configPath); err != nil {
			glog.Exit(fmt.Sprintf("Failed to set Kubeconfig path err %v\n", err))
		}
	}
	ma, err := vsphere.NewMachineActuator(token, nil, namedMachinesPath)
	if err != nil {
		glog.Exit(err)
	}
	return &deployer{
		token:           token,
		machineDeployer: ma,
		configPath:      configPath,
	}
}

func (d *deployer) CreateCluster(c *clusterv1.Cluster, machines []*clusterv1.Machine) error {
	vmCreated := false
	if err := d.createCluster(c, machines, &vmCreated); err != nil {
		if vmCreated {
			d.deleteMasterVM(c, machines)
		}
		d.machineDeployer.PostDelete(c, machines)
		return err
	}

	glog.Infof("The [%s] cluster has been created successfully!", c.Name)
	glog.Info("You can now `kubectl get nodes`")
	return nil
}

func (d *deployer) AddNodes(machines []*clusterv1.Machine) error {
	if err := d.createMachines(machines); err != nil {
		return err
	}
	return nil
}

func (d *deployer) DeleteCluster() error {
	if err := d.initApiClient(); err != nil {
		return err
	}

	machines, err := d.listMachines()
	if err != nil {
		return err
	}

	cluster, err := d.getCluster()
	if err != nil {
		return err
	}

	glog.Info("Deleting machine objects")
	if err := d.deleteAllMachines(); err != nil {
		return err
	}

	if err := d.deleteMasterVM(cluster, machines); err != nil {
		glog.Errorf("Error deleting master vm", err)
	}

	glog.Info("Running post delete operations")
	if err := d.machineDeployer.PostDelete(cluster, machines); err != nil {
		return err
	}
	glog.Infof("Deletion complete")
	return nil
}

func (d *deployer) deleteMasterVM(cluster *clusterv1.Cluster, machines []*clusterv1.Machine) error {
	master := util.GetMaster(machines)
	if master == nil {
		return fmt.Errorf("error deleting master vm, no master found")
	}

	glog.Infof("Deleting master vm %s", master.Name)
	if err := d.machineDeployer.Delete(cluster, master); err != nil {
		return err
	}
	return nil
}
