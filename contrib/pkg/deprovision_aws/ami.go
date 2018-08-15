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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	awsCredsSecretIDKey     = "awsAccessKeyId"
	awsCredsSecretAccessKey = "awsSecretAccessKey"
)

func (o *DeprovisionClusterOptions) DestroyAMI(clients *clientContext) error {
	ec2Client, err := getEC2Client(clients.KubeClient, o.AccountSecretName, o.Namespace, o.Region)
	if err != nil {
		o.Logger.WithError(err).Error("could not obtain ec2 client")
		return err
	}
	if len(o.AMIID) > 0 && len(o.AMIName) > 0 {
		o.Logger.WithField("ami-id", o.AMIID).Infof("AMI was created with cluster deployment. Deregistering")
		// An AMI was created and we know its ID
		return o.deregisterAMI(ec2Client, o.AMIID)
	}
	o.Logger.Infof("Could not obtain an AMI ID from cluster deployment. Looking for configmap with name")
	// A pod may have started image creation but it may have failed. We can retrieve
	// the AMI name from the configmap where it was initially saved.
	cm, err := clients.KubeClient.CoreV1().ConfigMaps(o.Namespace).Get(fmt.Sprintf("%s-aminame", o.Name), metav1.GetOptions{})
	if err != nil {
		o.Logger.WithError(err).Warning("Error trying to retrieve configmap for ami name")
		return nil
	}
	amiName := cm.Data["aminame"]
	o.Logger.WithField("ami-name", amiName).Infof("Obtained AMI name from configmap")

	// Do a best-effort to find/delete an instance that may have been used to create the AMI
	o.deleteInstanceIfPossible(ec2Client, amiName)

	// Delete AMI if possible
	o.deregisterAMIIfPossible(ec2Client, amiName)

	return nil
}

func (o *DeprovisionClusterOptions) deregisterAMI(ec2Client *ec2.EC2, id string) error {
	input := &ec2.DeregisterImageInput{
		ImageId: aws.String(id),
	}
	_, err := ec2Client.DeregisterImage(input)
	if err != nil {
		o.Logger.WithError(err).WithField("ami-id", id).Error("an error occurred deregistering image")
		return err
	}
	return nil
}

func (o *DeprovisionClusterOptions) deleteInstanceIfPossible(ec2Client *ec2.EC2, name string) {
	nameFilter := &ec2.Filter{
		Name:   aws.String("tag:Name"),
		Values: []*string{aws.String(name)},
	}
	describeInput := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{nameFilter},
	}
	o.Logger.WithField("ami-name", name).Infof("Looking for instance with AMI name")
	output, err := ec2Client.DescribeInstances(describeInput)
	if err != nil {
		o.Logger.WithError(err).WithField("instance-name", name).Error("an error occurred trying to find an instance")
	}
	if len(output.Reservations) == 0 || len(output.Reservations[0].Instances) == 0 {
		o.Logger.WithField("instance-name", name).Infof("No matching instances found")
		return
	}
	instanceID := *output.Reservations[0].Instances[0].InstanceId
	o.Logger.WithField("instance-id", instanceID).Info("Found instance with AMI name, deleting")
	o.deleteInstance(ec2Client, instanceID)
}

func (o *DeprovisionClusterOptions) deregisterAMIIfPossible(ec2Client *ec2.EC2, name string) {
	clusterIdFilter := &ec2.Filter{
		Name:   aws.String("tag:cluster-id"),
		Values: []*string{aws.String(name)},
	}
	describeInput := &ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{clusterIdFilter},
	}
	o.Logger.Infof("Looking for AMIs with tag cluster-id=%s", name)
	output, err := ec2Client.DescribeImages(describeInput)
	if err != nil {
		o.Logger.WithError(err).WithField("ami-name", name).Warning("an error occurred trying to find image")
		return
	}
	if len(output.Images) == 0 {
		o.Logger.WithField("ami-name", name).Infof("No matching images found")
		return
	}
	amiID := *output.Images[0].ImageId
	o.Logger.WithField("ami-id", amiID).Info("Found AMI. Deregistering it.")
	err = o.deregisterAMI(ec2Client, amiID)
	if err != nil {
		o.Logger.WithError(err).WithField("ami-id", amiID).Warning("Could not deregister AMI")
	}
}

func (o *DeprovisionClusterOptions) deleteInstance(ec2Client *ec2.EC2, id string) {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{aws.String(id)},
	}
	_, err := ec2Client.TerminateInstances(input)
	if err != nil {
		o.Logger.WithError(err).WithField("instance-id", id).Warning("Error deleting instance")
	}
}

func getEC2Client(kubeClient clientset.Interface, secretName, namespace, region string) (*ec2.EC2, error) {
	awsConfig := &aws.Config{Region: aws.String(region)}

	if secretName != "" {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(
			secretName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		accessKeyID, ok := secret.Data[awsCredsSecretIDKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				secretName, awsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[awsCredsSecretAccessKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				secretName, awsCredsSecretAccessKey)
		}

		awsConfig.Credentials = credentials.NewStaticCredentials(
			string(accessKeyID), string(secretAccessKey), "")
	}

	// Otherwise default to relying on the IAM role of the masters where the actuator is running:
	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}

	return ec2.New(s), nil
}
