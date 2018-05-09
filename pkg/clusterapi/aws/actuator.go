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
	"encoding/base64"
	"fmt"
	"io/ioutil"

	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/util"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	coapi "github.com/openshift/cluster-operator/pkg/api"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	// Path to bootstrap kubeconfig. This needs to be mounted to the controller pod
	// as a secret when running this controller.
	bootstrapKubeConfig = "/etc/origin/master/bootstrap.kubeconfig"

	// Hardcode IAM role for infra/compute for now
	defaultIAMRole = "openshift_node_describe_instances"

	// Annotation used to store serialized ClusterVersion resource in MachineSet
	clusterVersionAnnotation = "cluster-operator.openshift.io/cluster-version"

	// Instance ID annotation
	instanceIDAnnotation = "cluster-operator.openshift.io/aws-instance-id"
)

// Instance state constants
const (
	StatePending      = 0
	StateRunning      = 16
	StateShuttingDown = 32
	StateTerminated   = 48
	StateStopping     = 64
	StateStopped      = 80
)

var stateMask int64 = 0xFF

// Actuator is the AWS-specific actuator for the Cluster API machine controller
type Actuator struct {
	kubeClient              *kubernetes.Clientset
	clusterClient           *clusterclient.Clientset
	codecFactory            serializer.CodecFactory
	defaultAvailabilityZone string
	logger                  *log.Entry
}

// NewActuator returns a new AWS Actuator
func NewActuator(kubeClient *kubernetes.Clientset, clusterClient *clusterclient.Clientset, logger *log.Entry, defaultAvailabilityZone string) *Actuator {
	actuator := &Actuator{
		kubeClient:              kubeClient,
		clusterClient:           clusterClient,
		codecFactory:            coapi.Codecs,
		defaultAvailabilityZone: defaultAvailabilityZone,
		logger:                  logger,
	}
	return actuator
}

// Create runs a new EC2 instance
func (a *Actuator) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	a.logger.Debugf("Create %s/%s", machine.Namespace, machine.Name)
	result, err := a.CreateMachine(cluster, machine)
	if err != nil {
		a.logger.Errorf("error creating machine: %v", err)
		return err
	}
	machineCopy := machine.DeepCopy()
	if machineCopy.Annotations == nil {
		machineCopy.Annotations = map[string]string{}
	}
	machineCopy.Annotations[instanceIDAnnotation] = *(result.Instances[0].InstanceId)
	_, err = a.clusterClient.ClusterV1alpha1().Machines(machineCopy.Namespace).Update(machineCopy)
	if err != nil {
		a.logger.Errorf("error annotating new machine with instance ID: %v", err)
		return err
	}
	return err
}

// CreateMachine starts a new AWS instance as described by the cluster and machine resources
func (a *Actuator) CreateMachine(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*ec2.Reservation, error) {
	// Extract cluster operator cluster
	coCluster, err := a.clusterOperatorCluster(cluster)
	if err != nil {
		return nil, err
	}

	if coCluster.Spec.Hardware.AWS == nil {
		return nil, fmt.Errorf("Cluster does not contain an AWS hardware spec")
	}

	region := coCluster.Spec.Hardware.AWS.Region
	a.logger.Debugf("Obtaining EC2 client for region %q", region)
	client, err := a.ec2Client(region)
	if err != nil {
		return nil, fmt.Errorf("unable to obtain EC2 client: %v", err)
	}

	// For now, we store the machineSet resource in the machine.
	// It really should be the machine resource, but it's not yet
	// fleshed out in the Cluster Operator API
	coMachineSet, coClusterVersion, err := a.clusterOperatorMachineSet(machine)
	if err != nil {
		return nil, err
	}
	a.logger.Debugf("Creating a machine for machineset %q and cluster version %q", coMachineSet.Name, coClusterVersion.Name)

	if coClusterVersion.Spec.VMImages.AWSImages == nil {
		return nil, fmt.Errorf("cluster version does not contain AWS images")
	}

	// Get AMI to use
	amiName := amiForRegion(coClusterVersion.Spec.VMImages.AWSImages.RegionAMIs, region)
	if len(amiName) == 0 {
		return nil, fmt.Errorf("cannot determine AMI name from cluster version %q and region %s", coClusterVersion.Name, region)
	}

	a.logger.Debugf("Describing AMI %q", amiName)
	imageIds := []*string{aws.String(amiName)}
	describeImagesRequest := ec2.DescribeImagesInput{
		ImageIds: imageIds,
	}
	describeAMIResult, err := client.DescribeImages(&describeImagesRequest)
	if err != nil {
		return nil, fmt.Errorf("error describing AMI %s: %v", amiName, err)
	}
	a.logger.Debugf("Describe AMI result:\n%s", describeAMIResult)
	if len(describeAMIResult.Images) != 1 {
		return nil, fmt.Errorf("Unexpected number of images returned: %d", len(describeAMIResult.Images))
	}

	// Describe VPC
	vpcName := coCluster.Name
	vpcNameFilter := "tag:Name"
	describeVpcsRequest := ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{{Name: &vpcNameFilter, Values: []*string{&vpcName}}},
	}
	describeVpcsResult, err := client.DescribeVpcs(&describeVpcsRequest)
	if err != nil {
		return nil, fmt.Errorf("Error describing VPC %s: %v", vpcName, err)
	}
	a.logger.Debugf("Describe VPC result:\n%v", describeVpcsResult)
	if len(describeVpcsResult.Vpcs) != 1 {
		return nil, fmt.Errorf("Unexpected number of VPCs: %d", len(describeVpcsResult.Vpcs))
	}
	vpcID := *(describeVpcsResult.Vpcs[0].VpcId)

	// Describe Subnet
	describeSubnetsRequest := ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
		},
	}
	// Filter by default availability zone if one was passed, otherwise, take the first subnet
	// that comes back.
	if len(a.defaultAvailabilityZone) > 0 {
		describeSubnetsRequest.Filters = append(describeSubnetsRequest.Filters, &ec2.Filter{Name: aws.String("availability-zone"), Values: []*string{aws.String(a.defaultAvailabilityZone)}})
	}
	describeSubnetsResult, err := client.DescribeSubnets(&describeSubnetsRequest)
	if err != nil {
		return nil, fmt.Errorf("Error describing Subnets for VPC %s: %v", vpcName, err)
	}
	a.logger.Debugf("Describe Subnets result:\n%v", describeSubnetsResult)
	if len(describeSubnetsResult.Subnets) == 0 {
		return nil, fmt.Errorf("Did not find a subnet")
	}

	// Determine security groups
	var groupName, groupNameK8s string
	if coMachineSet.Spec.Infra {
		groupName = vpcName + "_infra"
		groupNameK8s = vpcName + "_infra_k8s"
	} else {
		groupName = vpcName + "_compute"
		groupNameK8s = vpcName + "_compute_k8s"
	}
	securityGroupNames := []*string{&vpcName, &groupName, &groupNameK8s}
	sgNameFilter := "group-name"
	describeSecurityGroupsRequest := ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("vpc-id"), Values: []*string{&vpcID}},
			{Name: &sgNameFilter, Values: securityGroupNames},
		},
	}
	describeSecurityGroupsResult, err := client.DescribeSecurityGroups(&describeSecurityGroupsRequest)
	if err != nil {
		return nil, err
	}
	a.logger.Debugf("Describe Security Groups result:\n%v", describeSecurityGroupsResult)

	var securityGroupIds []*string
	for _, g := range describeSecurityGroupsResult.SecurityGroups {
		groupID := *g.GroupId
		securityGroupIds = append(securityGroupIds, &groupID)
	}

	// build list of networkInterfaces (just 1 for now)
	var networkInterfaces = []*ec2.InstanceNetworkInterfaceSpecification{
		{
			DeviceIndex:              aws.Int64(0),
			AssociatePublicIpAddress: aws.Bool(true),
			SubnetId:                 describeSubnetsResult.Subnets[0].SubnetId,
			Groups:                   securityGroupIds,
		},
	}

	// Add tags to the created machine
	tagList := []*ec2.Tag{
		{Key: aws.String("clusterid"), Value: aws.String(coCluster.Name)},
		{Key: aws.String("kubernetes.io/cluster/" + coCluster.Name), Value: aws.String(coCluster.Name)},
		{Key: aws.String("Name"), Value: aws.String(machine.Name)},
	}
	tagInstance := &ec2.TagSpecification{
		ResourceType: aws.String("instance"),
		Tags:         tagList,
	}
	tagVolume := &ec2.TagSpecification{
		ResourceType: aws.String("volume"),
		Tags:         tagList[0:1],
	}

	// For now, these are fixed
	blkDeviceMappings := []*ec2.BlockDeviceMapping{
		{
			DeviceName: aws.String("/dev/sda1"),
			Ebs: &ec2.EbsBlockDevice{
				DeleteOnTermination: aws.Bool(true),
				VolumeSize:          aws.Int64(100),
				VolumeType:          aws.String("gp2"),
			},
		},
		{
			DeviceName: aws.String("/dev/sdb"),
			Ebs: &ec2.EbsBlockDevice{
				DeleteOnTermination: aws.Bool(true),
				VolumeSize:          aws.Int64(100),
				VolumeType:          aws.String("gp2"),
			},
		},
	}

	bootstrapKubeConfig, err := getBootstrapKubeconfig()
	if err != nil {
		return nil, fmt.Errorf("cannot get bootstrap kubeconfig: %v", err)
	}
	userData := getUserData(bootstrapKubeConfig, coMachineSet.Spec.Infra)
	userDataEnc := base64.StdEncoding.EncodeToString([]byte(userData))

	inputConfig := ec2.RunInstancesInput{
		ImageId:      describeAMIResult.Images[0].ImageId,
		InstanceType: aws.String(coMachineSet.Spec.Hardware.AWS.InstanceType),
		MinCount:     aws.Int64(1),
		MaxCount:     aws.Int64(1),
		UserData:     &userDataEnc,
		KeyName:      aws.String(coCluster.Spec.Hardware.AWS.KeyPairName),
		IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
			Name: aws.String(iamRole(coCluster)),
		},
		BlockDeviceMappings: blkDeviceMappings,
		TagSpecifications:   []*ec2.TagSpecification{tagInstance, tagVolume},
		NetworkInterfaces:   networkInterfaces,
	}

	runResult, err := client.RunInstances(&inputConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot create EC2 instance: %v", err)
	}
	a.logger.Debugf("Run Instances result:\n%v", runResult)

	return runResult, nil
}

// Delete deletes a machine and updates its finalizer
func (a *Actuator) Delete(machine *clusterv1.Machine) error {
	a.logger.Debugf("Delete %s/%s", machine.Namespace, machine.Name)
	if err := a.DeleteMachine(machine); err != nil {
		a.logger.Errorf("error deleting machine: %v", err)
		return err
	}

	// Deleting the machine was successful, remove the finalizer from the machine resource
	machineCopy := machine.DeepCopy()
	machineCopy.ObjectMeta.Finalizers = util.Filter(machineCopy.ObjectMeta.Finalizers, clusterv1.MachineFinalizer)
	_, err := a.clusterClient.ClusterV1alpha1().Machines(machineCopy.Namespace).Update(machineCopy)
	if err != nil {
		a.logger.Errorf("error removing finalizer from deleted machine: %v", err)
	}
	return err
}

// DeleteMachine deletes an AWS instance
func (a *Actuator) DeleteMachine(machine *clusterv1.Machine) error {
	a.logger.Debugf("DeleteMachine %s/%s", machine.Namespace, machine.Name)
	instanceID := getInstanceID(machine)
	if len(instanceID) == 0 {
		return nil
	}

	coMachineSet, _, err := a.clusterOperatorMachineSet(machine)
	if err != nil {
		return err
	}

	if coMachineSet.Spec.ClusterHardware.AWS == nil {
		return fmt.Errorf("machineSet does not contain AWS hardware spec")
	}
	region := coMachineSet.Spec.ClusterHardware.AWS.Region
	client, err := a.ec2Client(region)
	if err != nil {
		return fmt.Errorf("error getting EC2 client: %v", err)
	}

	terminateInstancesRequest := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}
	terminateInstancesResult, err := client.TerminateInstances(terminateInstancesRequest)
	if err != nil {
		return fmt.Errorf("error terminating instance %q: %v", instanceID, err)
	}
	a.logger.Debugf("Terminate Instances result:\n%v", terminateInstancesResult)
	return nil
}

// Update the machine to the provided definition.
// TODO: For now, this results in a No-op.
func (a *Actuator) Update(c *clusterv1.Cluster, machine *clusterv1.Machine) error {
	a.logger.Debugf("Update %s/%s", machine.Namespace, machine.Name)
	return nil
}

// Exists determines if the given machine currently exists.
func (a *Actuator) Exists(machine *clusterv1.Machine) (bool, error) {
	a.logger.Debugf("Exists %s/%s", machine.Namespace, machine.Name)
	instanceID := getInstanceID(machine)
	if len(instanceID) == 0 {
		return false, nil
	}

	coMachineSet, _, err := a.clusterOperatorMachineSet(machine)
	if err != nil {
		return false, err
	}

	if coMachineSet.Spec.ClusterHardware.AWS == nil {
		return false, fmt.Errorf("machineSet does not contain AWS hardware spec")
	}
	region := coMachineSet.Spec.ClusterHardware.AWS.Region
	client, err := a.ec2Client(region)
	if err != nil {
		return false, fmt.Errorf("error getting EC2 client: %v", err)
	}
	request := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("instance-id"), Values: []*string{&instanceID}},
		},
	}
	result, err := client.DescribeInstances(request)
	if err != nil {
		return false, err
	}
	a.logger.Debugf("Describe Instances result:\n%v", result)
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		return false, nil
	}
	if *(result.Reservations[0].Instances[0].InstanceId) == instanceID {
		// Determine whether the instance is in a running state, if not return false
		stateCode := *result.Reservations[0].Instances[0].State.Code
		stateCode = stateCode & stateMask // Only the lower byte is relevant
		if stateCode == StatePending || stateCode == StateRunning {
			return true, nil
		}
	}
	return false, nil
}

// Helper function to create an ec2 client
func (a *Actuator) ec2Client(region string) (*ec2.EC2, error) {
	s, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	if err != nil {
		return nil, err
	}
	return ec2.New(s), nil
}

func (a *Actuator) clusterOperatorCluster(c *clusterv1.Cluster) (*cov1.Cluster, error) {
	obj, _, err := a.codecFactory.UniversalDecoder(cov1.SchemeGroupVersion).Decode([]byte(c.Spec.ProviderConfig.Value.Raw), nil, nil)
	if err != nil {
		return nil, err
	}
	coCluster, ok := obj.(*cov1.Cluster)
	if !ok {
		return nil, fmt.Errorf("Unexpected object: %#v", obj)
	}
	return coCluster, nil
}

func (a *Actuator) clusterOperatorMachineSet(m *clusterv1.Machine) (*cov1.MachineSet, *cov1.ClusterVersion, error) {
	obj, _, err := a.codecFactory.UniversalDecoder(cov1.SchemeGroupVersion).Decode(m.Spec.ProviderConfig.Value.Raw, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	coMachineSet, ok := obj.(*cov1.MachineSet)
	if !ok {
		return nil, nil, fmt.Errorf("Unexpected machine set object: %#v", obj)
	}
	rawClusterVersion, ok := coMachineSet.Annotations[clusterVersionAnnotation]
	if !ok {
		return nil, nil, fmt.Errorf("Missing ClusterVersion resource annotation in MachineSet %#v", coMachineSet)
	}
	obj, _, err = a.codecFactory.UniversalDecoder(cov1.SchemeGroupVersion).Decode([]byte(rawClusterVersion), nil, nil)
	if err != nil {
		return nil, nil, err
	}
	coClusterVersion, ok := obj.(*cov1.ClusterVersion)
	if !ok {
		return nil, nil, fmt.Errorf("Unexpected cluster version object: %#v", obj)
	}

	return coMachineSet, coClusterVersion, nil
}

func amiForRegion(amis []cov1.AWSRegionAMIs, region string) string {
	for _, a := range amis {
		if a.Region == region {
			return a.AMI
		}
	}
	return ""
}

// template for user data
// takes the following parameters:
// 1 - type of machine (infra/compute)
// 2 - base64-encoded bootstrap.kubeconfig
const userDataTemplate = `#cloud-config
write_files:
- path: /root/openshift_bootstrap/openshift_settings.yaml
  owner: 'root:root'
  permissions: '0640'
  content: |
    openshift_group_type: %[1]s
- path: /etc/origin/node/bootstrap.kubeconfig
  owner: 'root:root'
  permissions: '0640'
  encoding: b64
  content: %[2]s
runcmd:
- [ ansible-playbook, /root/openshift_bootstrap/bootstrap.yml]
- [ systemctl, restart, systemd-hostnamed]
- [ systemctl, restart, NetworkManager]
- [ systemctl, enable, origin-node]
- [ systemctl, start, origin-node]
`

func getUserData(bootstrapKubeConfig string, infra bool) string {
	var nodeType string
	if infra {
		nodeType = "infra"
	} else {
		nodeType = "compute"
	}
	return fmt.Sprintf(userDataTemplate, nodeType, bootstrapKubeConfig)
}

func getBootstrapKubeconfig() (string, error) {
	content, err := ioutil.ReadFile(bootstrapKubeConfig)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(content), nil
}

func getInstanceID(machine *clusterv1.Machine) string {
	if machine.Annotations != nil {
		if instanceID, ok := machine.Annotations[instanceIDAnnotation]; ok {
			return instanceID
		}
	}
	return ""
}

func iamRole(cluster *cov1.Cluster) string {
	return fmt.Sprintf("%s_%s", defaultIAMRole, cluster.Name)
}
