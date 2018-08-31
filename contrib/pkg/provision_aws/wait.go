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

package provision_aws

import (
	"time"

	"github.com/openshift/cluster-operator/contrib/pkg/cluster"
)

func (o *ProvisionClusterOptions) WaitForCluster() error {
	opt := cluster.WaitForClusterOptions{
		Name:                      o.Name,
		Namespace:                 o.Namespace,
		ClusterResourceTimeout:    5 * time.Minute,
		InfraProvisioningTimeout:  10 * time.Minute,
		MasterInstallTimeout:      25 * time.Minute,
		ClusterAPIInstallTimeout:  10 * time.Minute,
		RemoteMachineSetsTimeout:  5 * time.Minute,
		RemoteNodesTimeout:        15 * time.Minute,
		RegistryDeploymentTimeout: 10 * time.Minute,
		RouterDeploymentTimeout:   10 * time.Minute,
		Logger:                    o.Logger,
	}
	return opt.WaitForCluster()
}
