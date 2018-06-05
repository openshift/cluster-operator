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

package aws

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

// SortInstances will examine the given slice of instances and return the current active instance for
// the machine, as well as a slice of all other instances which the caller may want to terminate. The
// active instance is calculated as the most recently launched instance.
// This function should only be called with running instances, not those which are stopped or
// terminated.
func SortInstances(instances []*ec2.Instance) (*ec2.Instance, []*ec2.Instance) {
	if len(instances) == 0 {
		return nil, []*ec2.Instance{}
	}
	var newestInstance *ec2.Instance
	inactiveInstances := make([]*ec2.Instance, 0, len(instances)-1)
	for _, i := range instances {
		if newestInstance == nil {
			newestInstance = i
			continue
		}
		tempInstance := chooseNewest(newestInstance, i)
		if *tempInstance.InstanceId != *newestInstance.InstanceId {
			inactiveInstances = append(inactiveInstances, newestInstance)
		} else {
			inactiveInstances = append(inactiveInstances, i)
		}
		newestInstance = tempInstance
	}
	return newestInstance, inactiveInstances
}

func chooseNewest(instance1, instance2 *ec2.Instance) *ec2.Instance {
	if instance1.LaunchTime == nil && instance2.LaunchTime == nil {
		// No idea what to do here, should not be possible, just return the first.
		return instance1
	}
	if instance1.LaunchTime != nil && instance2.LaunchTime == nil {
		return instance1
	}
	if instance1.LaunchTime == nil && instance2.LaunchTime != nil {
		return instance2
	}
	if (*instance1.LaunchTime).After(*instance2.LaunchTime) {
		return instance1
	}
	return instance2
}

// GetInstance returns the AWS instance for a given machine. If multiple instances match our machine,
// the most recently launched will be returned. If no instance exists, an error will be returned.
func GetInstance(machine *clusterv1.Machine, client ec2iface.EC2API) (*ec2.Instance, error) {
	instances, err := GetRunningInstances(machine, client)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no instance found for machine: %s", machine.Name)
	}

	instance, _ := SortInstances(instances)
	return instance, nil
}

// GetRunningInstances returns all running instances that have a tag matching our machine name,
// and cluster ID.
func GetRunningInstances(machine *clusterv1.Machine, client ec2iface.EC2API) ([]*ec2.Instance, error) {

	machineName := machine.Name

	clusterID, ok := getClusterID(machine)
	if !ok {
		return []*ec2.Instance{}, fmt.Errorf("unable to get cluster ID for machine: %s", machine.Name)
	}

	// Query instances with our machine's name, and in running/pending state.
	request := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []*string{&machineName},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []*string{aws.String("running"), aws.String("pending")},
			},
			{
				Name:   aws.String("tag:clusterid"),
				Values: []*string{&clusterID},
			},
		},
	}
	result, err := client.DescribeInstances(request)
	if err != nil {
		return []*ec2.Instance{}, err
	}

	instances := make([]*ec2.Instance, 0, len(result.Reservations))
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// CreateAWSClients creates clients for the AWS services we use, using either the cluster AWS credentials secret
// if defined (i.e. in the root cluster), otherwise the IAM profile of the master where the
// actuator will run. (target clusters)
func CreateAWSClients(kubeClient kubernetes.Interface, mSpec *cov1.MachineSetSpec, namespace, region string) (ec2iface.EC2API, elbiface.ELBAPI, error) {
	awsConfig := &aws.Config{Region: aws.String(region)}

	if mSpec.ClusterHardware.AWS == nil {
		return nil, nil, fmt.Errorf("no AWS cluster hardware set on machine spec")
	}

	// If the cluster specifies an AWS credentials secret and it exists, use it for our client credentials:
	if mSpec.ClusterHardware.AWS.AccountSecret.Name != "" {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(
			mSpec.ClusterHardware.AWS.AccountSecret.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		accessKeyID, ok := secret.Data[awsCredsSecretIDKey]
		if !ok {
			return nil, nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				mSpec.ClusterHardware.AWS.AccountSecret.Name, awsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[awsCredsSecretAccessKey]
		if !ok {
			return nil, nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				mSpec.ClusterHardware.AWS.AccountSecret.Name, awsCredsSecretAccessKey)
		}

		awsConfig.Credentials = credentials.NewStaticCredentials(
			string(accessKeyID), string(secretAccessKey), "")
	}

	// Otherwise default to relying on the IAM role of the masters where the actuator is running:
	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, nil, err
	}
	return ec2.New(s), elb.New(s), nil
}

// TerminateInstances terminates all provided instances with a single EC2 request.
func TerminateInstances(client ec2iface.EC2API, instances []*ec2.Instance, mLog log.FieldLogger) error {
	instanceIDs := []*string{}
	// Cleanup all older instances:
	for _, instance := range instances {
		mLog.WithFields(log.Fields{
			"instanceID": *instance.InstanceId,
			"state":      *instance.State.Name,
			"launchTime": *instance.LaunchTime,
		}).Warn("cleaning up extraneous instance for machine")
		instanceIDs = append(instanceIDs, instance.InstanceId)
	}
	for _, instanceID := range instanceIDs {
		mLog.WithField("instanceID", *instanceID).Info("terminating instance")
	}

	terminateInstancesRequest := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	}
	_, err := client.TerminateInstances(terminateInstancesRequest)
	if err != nil {
		mLog.Errorf("error terminating instances: %v", err)
		return fmt.Errorf("error terminating instances: %v", err)
	}
	return nil
}
