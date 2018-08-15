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

package deprovision_aws

import (
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (o *DeprovisionClusterOptions) DestroyCluster(clients *clientContext) error {
	// Check if cluster deployment exists
	cd, err := clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterDeployments(o.Namespace).Get(o.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			o.Logger.Info("ClusterDeployment not found. Nothing to do to delete cluster")
			return nil
		}
		o.Logger.WithError(err).Error("Error while trying to retrieve ClusterDeployment")
		return err
	}
	o.AMIID = cd.Annotations["clusteroperator.openshift.io/ami-id"]
	o.AMIName = cd.Annotations["clusteroperator.openshift.io/ami-name"]
	o.Logger.WithField("ami-id", o.AMIID).WithField("ami-name", o.AMIName).Info("Retrieved annotations from cluster deployment")
	if cd.DeletionTimestamp.IsZero() {
		o.Logger.Info("Cluster deployment exists.")
		err := clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterDeployments(o.Namespace).Delete(o.Name, &metav1.DeleteOptions{})
		if err != nil {
			o.Logger.WithError(err).Error("Error while deleting clusterdeployment")
		}
		o.Logger.Infof("Started deletion of clusterdeployment.")
	}

	o.Logger.Infof("Waiting for cluster deployment to finish deleting")
	return wait.PollImmediate(5*time.Second, 30*time.Minute, func() (bool, error) {
		var err error
		_, err = clients.ClusterOpClient.ClusteroperatorV1alpha1().ClusterDeployments(o.Namespace).Get(o.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			o.Logger.Info("ClusterDeployment has been deleted")
			return true, nil
		}
		if err != nil {
			o.Logger.WithError(err).Error("unexpected error retrieving ClusterDeployment")
			return false, err
		}
		o.Logger.Debug("ClusterDeployment has not been deleted. Continuing to wait")
		return false, nil
	})
}
