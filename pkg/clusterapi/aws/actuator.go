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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	coapi "github.com/openshift/cluster-operator/pkg/api"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	clustoplog "github.com/openshift/cluster-operator/pkg/logging"
)

const (
	// Path to bootstrap kubeconfig. This needs to be mounted to the controller pod
	// as a secret when running this controller.
	bootstrapKubeConfig = "/etc/origin/master/bootstrap.kubeconfig"

	// Hardcode IAM role for infra/compute for now
	defaultIAMRole = "openshift_node_describe_instances"

	// Instance ID annotation
	instanceIDAnnotation = "cluster-operator.openshift.io/aws-instance-id"

	awsCredsSecretIDKey     = "awsAccessKeyId"
	awsCredsSecretAccessKey = "awsSecretAccessKey"

	ec2InstanceIDNotFoundCode = "InvalidInstanceID.NotFound"
)

// Instance state constants
const (
	StatePending       = 0
	StateRunning       = 16
	StateShuttingDown  = 32
	StateTerminated    = 48
	StateStopping      = 64
	StateStopped       = 80
	hostTypeNode       = "node"
	hostTypeMaster     = "master"
	subHostTypeDefault = "default"
	subHostTypeInfra   = "infra"
	subHostTypeCompute = "compute"
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
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("Create %s/%s", machine.Namespace, machine.Name)
	result, err := a.CreateMachine(cluster, machine)
	if err != nil {
		mLog.Errorf("error creating machine: %v", err)
		return err
	}
	machineCopy := machine.DeepCopy()
	if machineCopy.Annotations == nil {
		machineCopy.Annotations = map[string]string{}
	}
	machineCopy.Annotations[instanceIDAnnotation] = *(result.Instances[0].InstanceId)
	_, err = a.clusterClient.ClusterV1alpha1().Machines(machineCopy.Namespace).Update(machineCopy)
	if err != nil {
		// TODO: if this fails because machine updated in meantime, we requeue machine, see no instance ID,
		// and create another.
		a.logger.Errorf("error annotating new machine with instance ID: %v", err)
		return err
	}
	return err
}

func machineHasRole(machine *clusterv1.Machine, role capicommon.MachineRole) bool {
	for _, r := range machine.Spec.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// CreateMachine starts a new AWS instance as described by the cluster and machine resources
func (a *Actuator) CreateMachine(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*ec2.Reservation, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)
	// Extract cluster operator cluster
	clusterSpec, err := controller.ClusterSpecFromClusterAPI(cluster)
	if err != nil {
		return nil, err
	}

	if clusterSpec.Hardware.AWS == nil {
		return nil, fmt.Errorf("Cluster does not contain an AWS hardware spec")
	}

	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		return nil, err
	}
	mLog.Debugf("Creating machine %q", machine.Name)

	region := clusterSpec.Hardware.AWS.Region
	mLog.Debugf("Obtaining EC2 client for region %q", region)
	client, err := a.ec2Client(coMachineSetSpec, machine.Namespace, region)
	if err != nil {
		return nil, fmt.Errorf("unable to obtain EC2 client: %v", err)
	}

	if coMachineSetSpec.VMImage.AWSImage == nil {
		return nil, fmt.Errorf("machine does not have an AWS image set")
	}

	// Get AMI to use
	amiName := *coMachineSetSpec.VMImage.AWSImage

	mLog.Debugf("Describing AMI %s", amiName)
	imageIds := []*string{aws.String(amiName)}
	describeImagesRequest := ec2.DescribeImagesInput{
		ImageIds: imageIds,
	}
	describeAMIResult, err := client.DescribeImages(&describeImagesRequest)
	if err != nil {
		return nil, fmt.Errorf("error describing AMI %s: %v", amiName, err)
	}
	if len(describeAMIResult.Images) != 1 {
		return nil, fmt.Errorf("Unexpected number of images returned: %d", len(describeAMIResult.Images))
	}

	// Describe VPC
	vpcName := cluster.Name
	vpcNameFilter := "tag:Name"
	describeVpcsRequest := ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{{Name: &vpcNameFilter, Values: []*string{&vpcName}}},
	}
	describeVpcsResult, err := client.DescribeVpcs(&describeVpcsRequest)
	if err != nil {
		return nil, fmt.Errorf("Error describing VPC %s: %v", vpcName, err)
	}
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
	mLog.Debugf("Describe Subnets result:\n%v", describeSubnetsResult)
	if len(describeSubnetsResult.Subnets) == 0 {
		return nil, fmt.Errorf("Did not find a subnet")
	}

	// Determine security groups
	var groupName, groupNameK8s string
	if coMachineSetSpec.Infra {
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
	mLog.Debugf("Describe Security Groups result:\n%v", describeSecurityGroupsResult)

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

	// Set host type and sub-type to match what we do in pkg/ansible/generate.go. This is required
	// mainly for our Ansible code that dynamically generates the list of masters by searching for
	// AWS tags.
	hostType := hostTypeNode
	subHostType := subHostTypeCompute
	if machineHasRole(machine, capicommon.MasterRole) {
		hostType = hostTypeMaster
		subHostType = subHostTypeDefault
	}
	if coMachineSetSpec.Infra {
		subHostType = subHostTypeInfra
	}
	mLog.WithFields(log.Fields{"hostType": hostType, "subHostType": subHostType}).Debugf("creating instance with host type")

	// Add tags to the created machine
	tagList := []*ec2.Tag{
		{Key: aws.String("clusterid"), Value: aws.String(cluster.Name)},
		{Key: aws.String("host-type"), Value: aws.String(hostType)},
		{Key: aws.String("sub-host-type"), Value: aws.String(subHostType)},
		{Key: aws.String("kubernetes.io/cluster/" + cluster.Name), Value: aws.String(cluster.Name)},
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
	userData := getUserData(bootstrapKubeConfig, coMachineSetSpec.Infra)
	userDataEnc := base64.StdEncoding.EncodeToString([]byte(userData))

	inputConfig := ec2.RunInstancesInput{
		ImageId:      describeAMIResult.Images[0].ImageId,
		InstanceType: aws.String(coMachineSetSpec.Hardware.AWS.InstanceType),
		MinCount:     aws.Int64(1),
		MaxCount:     aws.Int64(1),
		UserData:     &userDataEnc,
		KeyName:      aws.String(clusterSpec.Hardware.AWS.KeyPairName),
		IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
			Name: aws.String(iamRole(cluster)),
		},
		BlockDeviceMappings: blkDeviceMappings,
		TagSpecifications:   []*ec2.TagSpecification{tagInstance, tagVolume},
		NetworkInterfaces:   networkInterfaces,
	}

	runResult, err := client.RunInstances(&inputConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot create EC2 instance: %v", err)
	}

	return runResult, nil
}

// Delete deletes a machine and updates its finalizer
func (a *Actuator) Delete(machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("Delete %s/%s", machine.Namespace, machine.Name)
	if err := a.DeleteMachine(machine); err != nil {
		a.logger.Errorf("error deleting machine: %v", err)
		return err
	}
	return nil
}

// DeleteMachine deletes an AWS instance
func (a *Actuator) DeleteMachine(machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("DeleteMachine %s/%s", machine.Namespace, machine.Name)
	// TODO: should we lookup all instances matching name and clean them all up?
	instanceID := getInstanceID(machine)
	if len(instanceID) == 0 {
		mLog.Warnf("attempted to delete machine with no instance ID set")
		return nil
	}

	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		return err
	}

	if coMachineSetSpec.ClusterHardware.AWS == nil {
		return fmt.Errorf("machine does not contain AWS hardware spec")
	}
	region := coMachineSetSpec.ClusterHardware.AWS.Region
	client, err := a.ec2Client(coMachineSetSpec, machine.Namespace, region)
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
		// If the instance no longer exists, we need to make sure we don't infinitely
		// block deletion of the Machine in etcd.
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == ec2InstanceIDNotFoundCode {
				mLog.WithField("instanceID", instanceID).Warnf("AWS instance no longer exists")
				return nil
			}
		}
		return fmt.Errorf("error terminating instance %q: %v", instanceID, err)
	}
	a.logger.Debugf("Terminate Instances result:\n%v", terminateInstancesResult)
	return nil
}

// Update the machine to the provided definition.
// TODO: For now, this results in a No-op. We should check for the latest correct instance (name matches, in running/pending state)
func (a *Actuator) Update(c *clusterv1.Cluster, machine *clusterv1.Machine) error {
	a.logger.Debugf("Update %s/%s", machine.Namespace, machine.Name)
	return nil
}

// Exists determines if the given machine currently exists.
func (a *Actuator) Exists(machine *clusterv1.Machine) (bool, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("checking if machine exists")

	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		return false, err
	}

	if coMachineSetSpec.ClusterHardware.AWS == nil {
		return false, fmt.Errorf("machineSet does not contain AWS hardware spec")
	}
	region := coMachineSetSpec.ClusterHardware.AWS.Region
	client, err := a.ec2Client(coMachineSetSpec, machine.Namespace, region)
	if err != nil {
		return false, fmt.Errorf("error getting EC2 client: %v", err)
	}
	machineName := machine.Name

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
		},
	}
	result, err := client.DescribeInstances(request)
	if err != nil {
		return false, err
	}
	// No instances found with this name, definitely does not exist:
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		mLog.Debug("instance does not exist")
		return false, nil
	}

	if len(result.Reservations) > 1 {
		// This is not good. Not sure if or where we could even handle it.
		// TODO: can we issue a delete on one here? the oldest? reconcile in Update instead?
		for _, reservation := range result.Reservations {
			for _, instance := range reservation.Instances {
				mLog.WithFields(log.Fields{
					"instanceID": *instance.InstanceId,
					"state":      *instance.State.Name,
				}).Warn("found multiple instances for machine in pending/running state")
			}
		}
	}

	mLog.Debug("instance exists")
	return true, nil
}

// ec2Client creates an EC2 client using either the cluster AWS credentials secret
// if defined (i.e. in the root cluster), otherwise the IAM profile of the master where the
// actuator will run. (target clusters)
func (a *Actuator) ec2Client(mSpec *cov1.MachineSetSpec, namespace, region string) (*ec2.EC2, error) {
	awsConfig := &aws.Config{Region: aws.String(region)}

	if mSpec.ClusterHardware.AWS == nil {
		return nil, fmt.Errorf("no AWS cluster hardware set on machine spec")
	}

	// If the cluster specifies an AWS credentials secret and it exists, use it for our client credentials:
	if mSpec.ClusterHardware.AWS.AccountSecret.Name != "" {
		a.logger.Debugf("loading AWS credentials secret")
		secret, err := a.kubeClient.CoreV1().Secrets(namespace).Get(
			mSpec.ClusterHardware.AWS.AccountSecret.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		accessKeyID, ok := secret.Data[awsCredsSecretIDKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				mSpec.ClusterHardware.AWS.AccountSecret.Name, awsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[awsCredsSecretAccessKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				mSpec.ClusterHardware.AWS.AccountSecret.Name, awsCredsSecretAccessKey)
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

func iamRole(cluster *clusterv1.Cluster) string {
	return fmt.Sprintf("%s", defaultIAMRole)
}
