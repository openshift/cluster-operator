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
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"text/template"

	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"

	"github.com/aws/aws-sdk-go/aws"
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

	// IAM role for infra/compute
	defaultIAMRole = "openshift_node_describe_instances"

	// IAM role for master
	masterIAMRole = "openshift_master_launch_instances"

	// Instance ID annotation
	instanceIDAnnotation = "cluster-operator.openshift.io/aws-instance-id"

	awsCredsSecretIDKey     = "awsAccessKeyId"
	awsCredsSecretAccessKey = "awsSecretAccessKey"

	ec2InstanceIDNotFoundCode = "InvalidInstanceID.NotFound"
)

// Instance tag constants
// TODO: these do not match the case of the clustop or capi role names
const (
	hostTypeNode       = "node"
	hostTypeMaster     = "master"
	subHostTypeDefault = "default"
	subHostTypeInfra   = "infra"
	subHostTypeCompute = "compute"
)

var stateMask int64 = 0xFF

// Actuator is the AWS-specific actuator for the Cluster API machine controller
type Actuator struct {
	kubeClient              kubernetes.Interface
	clusterClient           clusterclient.Interface
	codecFactory            serializer.CodecFactory
	defaultAvailabilityZone string
	logger                  *log.Entry
	clientBuilder           func(kubeClient kubernetes.Interface, mSpec *cov1.MachineSetSpec, namespace, region string) (Client, error)
	userDataGenerator       func(master, infra bool) (string, error)
}

// NewActuator returns a new AWS Actuator
func NewActuator(kubeClient kubernetes.Interface, clusterClient clusterclient.Interface, logger *log.Entry, defaultAvailabilityZone string) *Actuator {
	actuator := &Actuator{
		kubeClient:              kubeClient,
		clusterClient:           clusterClient,
		codecFactory:            coapi.Codecs,
		defaultAvailabilityZone: defaultAvailabilityZone,
		logger:                  logger,
		clientBuilder:           NewClient,
		userDataGenerator:       generateUserData,
	}
	return actuator
}

// Create runs a new EC2 instance
func (a *Actuator) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Info("creating machine")
	instance, err := a.CreateMachine(cluster, machine)
	if err != nil {
		mLog.Errorf("error creating machine: %v", err)
		return err
	}

	return a.updateStatus(machine, instance, mLog)
}

// removeStoppedMachine removes all instances of a specific machine that are in a stopped state.
func (a *Actuator) removeStoppedMachine(machine *clusterv1.Machine, client Client, mLog log.FieldLogger) error {
	instances, err := GetStoppedInstances(machine, client)
	if err != nil {
		return fmt.Errorf("Error getting stopped instances: %v", err)
	}

	if len(instances) == 0 {
		mLog.Infof("no stopped instances found for machine %v", machine.Name)
		return nil
	}

	return TerminateInstances(client, instances, mLog)
}

// CreateMachine starts a new AWS instance as described by the cluster and machine resources
func (a *Actuator) CreateMachine(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*ec2.Instance, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)

	isMaster := controller.MachineHasRole(machine, capicommon.MasterRole)

	// Extract cluster operator cluster
	clusterSpec, err := controller.ClusterDeploymentSpecFromCluster(cluster)
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

	region := clusterSpec.Hardware.AWS.Region
	mLog.Debugf("Obtaining EC2 client for region %q", region)
	client, err := a.clientBuilder(a.kubeClient, coMachineSetSpec, machine.Namespace, region)
	if err != nil {
		return nil, fmt.Errorf("unable to obtain EC2 client: %v", err)
	}

	// We explicitly do NOT want to remove stopped masters.
	if !isMaster {
		// Prevent having a lot of stopped nodes sitting around.
		err = a.removeStoppedMachine(machine, client, mLog)
		if err != nil {
			return nil, fmt.Errorf("unable to remove stopped nodes: %v", err)
		}
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
	vpcName := clusterSpec.ClusterID
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
	if len(describeSubnetsResult.Subnets) == 0 {
		return nil, fmt.Errorf("Did not find a subnet")
	}

	// Determine security groups
	describeSecurityGroupsInput := buildDescribeSecurityGroupsInput(vpcID, vpcName, controller.MachineHasRole(machine, capicommon.MasterRole), coMachineSetSpec.Infra)
	describeSecurityGroupsOutput, err := client.DescribeSecurityGroups(describeSecurityGroupsInput)
	if err != nil {
		return nil, err
	}

	var securityGroupIds []*string
	for _, g := range describeSecurityGroupsOutput.SecurityGroups {
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
	if isMaster {
		hostType = hostTypeMaster
		subHostType = subHostTypeDefault
	}
	if coMachineSetSpec.Infra {
		subHostType = subHostTypeInfra
	}
	mLog.WithFields(log.Fields{"hostType": hostType, "subHostType": subHostType}).Debugf("creating instance with host type")

	// Add tags to the created machine
	tagList := []*ec2.Tag{
		{Key: aws.String("clusterid"), Value: aws.String(clusterSpec.ClusterID)},
		{Key: aws.String("host-type"), Value: aws.String(hostType)},
		{Key: aws.String("sub-host-type"), Value: aws.String(subHostType)},
		{Key: aws.String("kubernetes.io/cluster/" + clusterSpec.ClusterID), Value: aws.String(clusterSpec.ClusterID)},
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

	// Only compute nodes should get user data, and it's quite important that masters do not as the
	// AWS actuator for these is running on the root CO cluster currently, and we do not want to leak
	// root CO cluster bootstrap kubeconfigs to the target cluster.
	userData, err := a.userDataGenerator(controller.MachineHasRole(machine, capicommon.MasterRole), coMachineSetSpec.Infra)
	if err != nil {
		return nil, err
	}
	userDataEnc := base64.StdEncoding.EncodeToString([]byte(userData))

	inputConfig := ec2.RunInstancesInput{
		ImageId:      describeAMIResult.Images[0].ImageId,
		InstanceType: aws.String(coMachineSetSpec.Hardware.AWS.InstanceType),
		MinCount:     aws.Int64(1),
		MaxCount:     aws.Int64(1),
		KeyName:      aws.String(clusterSpec.Hardware.AWS.KeyPairName),
		IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
			Name: aws.String(iamRole(machine)),
		},
		BlockDeviceMappings: blkDeviceMappings,
		TagSpecifications:   []*ec2.TagSpecification{tagInstance, tagVolume},
		NetworkInterfaces:   networkInterfaces,
		UserData:            &userDataEnc,
	}

	runResult, err := client.RunInstances(&inputConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot create EC2 instance: %v", err)
	}

	if runResult == nil || len(runResult.Instances) != 1 {
		mLog.Errorf("unexpected reservation creating instances: %v", runResult)
		return nil, fmt.Errorf("unexpected reservation creating instance")
	}
	return runResult.Instances[0], nil
}

// Delete deletes a machine and updates its finalizer
func (a *Actuator) Delete(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Info("deleting machine")
	if err := a.DeleteMachine(machine); err != nil {
		mLog.Errorf("error deleting machine: %v", err)
		return err
	}
	return nil
}

// DeleteMachine deletes an AWS instance
func (a *Actuator) DeleteMachine(machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)

	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		return err
	}

	if coMachineSetSpec.ClusterHardware.AWS == nil {
		return fmt.Errorf("machine does not contain AWS hardware spec")
	}
	region := coMachineSetSpec.ClusterHardware.AWS.Region
	client, err := a.clientBuilder(a.kubeClient, coMachineSetSpec, machine.Namespace, region)
	if err != nil {
		return fmt.Errorf("error getting EC2 client: %v", err)
	}

	instances, err := GetRunningInstances(machine, client)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		mLog.Warnf("no instances found to delete for machine")
		return nil
	}

	return TerminateInstances(client, instances, mLog)
}

// Update attempts to sync machine state with an existing instance. Today this just updates status
// for details that may have changed. (IPs and hostnames) We do not currently support making any
// changes to actual machines in AWS. Instead these will be replaced via MachineDeployments.
func (a *Actuator) Update(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("updating machine")

	clusterSpec, err := controller.ClusterDeploymentSpecFromCluster(cluster)
	if err != nil {
		return err
	}

	if clusterSpec.Hardware.AWS == nil {
		return fmt.Errorf("Cluster does not contain an AWS hardware spec")
	}

	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		return err
	}

	region := clusterSpec.Hardware.AWS.Region
	mLog.WithField("region", region).Debugf("obtaining EC2 client for region")
	client, err := a.clientBuilder(a.kubeClient, coMachineSetSpec, machine.Namespace, region)
	if err != nil {
		return fmt.Errorf("unable to obtain EC2 client: %v", err)
	}

	instances, err := GetRunningInstances(machine, client)
	mLog.Debugf("found %d instances for machine", len(instances))
	if err != nil {
		return err
	}

	// Parent controller should prevent this from ever happening by calling Exists and then Create,
	// but instance could be deleted between the two calls.
	if len(instances) == 0 {
		mLog.Warnf("attempted to update machine but no instances found")
		// Update status to clear out machine details.
		err := a.updateStatus(machine, nil, mLog)
		if err != nil {
			return err
		}
		return fmt.Errorf("attempted to update machine but no instances found")
	}
	newestInstance, terminateInstances := SortInstances(instances)

	// In very unusual circumstances, there could be more than one machine running matching this
	// machine name and cluster ID. In this scenario we will keep the newest, and delete all others.
	mLog = mLog.WithField("instanceID", *newestInstance.InstanceId)
	mLog.Debug("instance found")

	if len(instances) > 1 {
		err = TerminateInstances(client, terminateInstances, mLog)
		if err != nil {
			return err
		}

	}

	// We do not support making changes to pre-existing instances, just update status.
	return a.updateStatus(machine, newestInstance, mLog)
}

// Exists determines if the given machine currently exists. For AWS we query for instances in
// running state, with a matching name tag, to determine a match.
func (a *Actuator) Exists(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (bool, error) {
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
	client, err := a.clientBuilder(a.kubeClient, coMachineSetSpec, machine.Namespace, region)
	if err != nil {
		return false, fmt.Errorf("error getting EC2 client: %v", err)
	}

	instances, err := GetRunningInstances(machine, client)
	if err != nil {
		return false, err
	}
	if len(instances) == 0 {
		mLog.Debug("instance does not exist")
		return false, nil
	}

	// If more than one result was returned, it will be handled in Update.
	mLog.Debug("instance exists")
	return true, nil
}

// updateStatus calculates the new machine status, checks if anything has changed, and updates if so.
func (a *Actuator) updateStatus(machine *clusterv1.Machine, instance *ec2.Instance, mLog log.FieldLogger) error {

	mLog.Debug("updating status")

	// Starting with a fresh status as we assume full control of it here.
	awsStatus, err := controller.AWSMachineProviderStatusFromClusterAPIMachine(machine)
	if err != nil {
		return err
	}
	// Save this, we need to check if it changed later.
	origInstanceID := awsStatus.InstanceID

	// Instance may have existed but been deleted outside our control, clear it's status if so:
	if instance == nil {
		awsStatus.InstanceID = nil
		awsStatus.InstanceState = nil
		awsStatus.PublicIP = nil
		awsStatus.PublicDNS = nil
		awsStatus.PrivateIP = nil
		awsStatus.PrivateDNS = nil
	} else {
		awsStatus.InstanceID = instance.InstanceId
		awsStatus.InstanceState = instance.State.Name
		// Some of these pointers may still be nil (public IP and DNS):
		awsStatus.PublicIP = instance.PublicIpAddress
		awsStatus.PublicDNS = instance.PublicDnsName
		awsStatus.PrivateIP = instance.PrivateIpAddress
		awsStatus.PrivateDNS = instance.PrivateDnsName
	}
	mLog.Debug("finished calculating AWS status")

	if !controller.StringPtrsEqual(origInstanceID, awsStatus.InstanceID) {
		mLog.Debug("AWS instance ID changed, clearing LastELBSync to trigger adding to ELBs")
		awsStatus.LastELBSync = nil
	}

	awsStatusRaw, err := controller.ClusterAPIMachineProviderStatusFromAWSMachineProviderStatus(awsStatus)
	if err != nil {
		mLog.Errorf("error encoding AWS provider status: %v", err)
		return err
	}

	machineCopy := machine.DeepCopy()
	machineCopy.Status.ProviderStatus = awsStatusRaw

	if !equality.Semantic.DeepEqual(machine.Status, machineCopy.Status) {
		mLog.Info("machine status has changed, updating")
		machineCopy.Status.LastUpdated = metav1.Now()

		_, err := a.clusterClient.ClusterV1alpha1().Machines(machineCopy.Namespace).UpdateStatus(machineCopy)
		if err != nil {
			mLog.Errorf("error updating machine status: %v", err)
			return err
		}
	} else {
		mLog.Debug("status unchanged")
	}
	return nil
}

func getClusterID(machine *clusterv1.Machine) (string, error) {
	coMachineSetSpec, err := controller.MachineSetSpecFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		return "", err
	}
	return coMachineSetSpec.ClusterID, nil
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
    openshift_group_type: {{ .NodeType }}
{{- if .IsNode }}
- path: /etc/origin/node/bootstrap.kubeconfig
  owner: 'root:root'
  permissions: '0640'
  encoding: b64
  content: {{ .BootstrapKubeconfig }}
{{- end }}
runcmd:
- [ ansible-playbook, /root/openshift_bootstrap/bootstrap.yml]
{{- if .IsNode }}
- [ systemctl, restart, systemd-hostnamed]
- [ systemctl, restart, NetworkManager]
- [ systemctl, enable, origin-node]
- [ systemctl, start, origin-node]
{{- end }}`

type userDataParams struct {
	NodeType            string
	BootstrapKubeconfig string
	IsNode              bool
}

func executeTemplate(isMaster, isInfra bool, bootstrapKubeconfig string) (string, error) {
	var nodeType string
	if isMaster {
		nodeType = "master"
	} else if isInfra {
		nodeType = "infra"
	} else {
		nodeType = "compute"
	}
	params := userDataParams{
		NodeType:            nodeType,
		BootstrapKubeconfig: bootstrapKubeconfig,
		IsNode:              !isMaster,
	}

	t, err := template.New("userdata").Parse(userDataTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// generateUserData is a generator function used in the actuator to create the user data for a
// specific type of machine.
func generateUserData(isMaster, isInfra bool) (string, error) {
	var bootstrapKubeconfig string
	var err error
	if !isMaster {
		bootstrapKubeconfig, err = getBootstrapKubeconfig()
		if err != nil {
			return "", fmt.Errorf("cannot get bootstrap kubeconfig: %v", err)
		}
	}

	return executeTemplate(isMaster, isInfra, bootstrapKubeconfig)
}

// getBootstrapKubeconfig reads the bootstrap kubeconfig expected to be mounted into the pod. This assumes
// the actuator runs on a master which has such a kubeconfig for joining nodes to the cluster.
func getBootstrapKubeconfig() (string, error) {
	content, err := ioutil.ReadFile(bootstrapKubeConfig)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(content), nil
}

func iamRole(machine *clusterv1.Machine) string {
	if controller.MachineHasRole(machine, capicommon.MasterRole) {
		return masterIAMRole
	}
	return defaultIAMRole
}

func buildDescribeSecurityGroupsInput(vpcID, vpcName string, isMaster, isInfra bool) *ec2.DescribeSecurityGroupsInput {
	groupNames := []*string{aws.String(vpcName)}
	if isMaster {
		groupNames = append(groupNames, aws.String(vpcName+"_master"))
		groupNames = append(groupNames, aws.String(vpcName+"_master_k8s"))
	}
	if isInfra {
		groupNames = append(groupNames, aws.String(vpcName+"_infra"))
		groupNames = append(groupNames, aws.String(vpcName+"_infra_k8s"))
	}
	if !isMaster && !isInfra {
		groupNames = append(groupNames, aws.String(vpcName+"_compute"))
		groupNames = append(groupNames, aws.String(vpcName+"_compute_k8s"))
	}

	return &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("vpc-id"), Values: []*string{&vpcID}},
			{Name: aws.String("group-name"), Values: groupNames},
		},
	}
}
