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
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	clustopv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	mockaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws/mock"
	"github.com/openshift/cluster-operator/pkg/controller"

	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientfake "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

const (
	testNamespace             = "test-namespace"
	testClusterDeploymentName = "test-cluster-deployment"
	testClusterDeploymentUUID = types.UID("test-cluster-deployment-uuid")
	testClusterID             = "test-cluster-id"
	testClusterVerName        = "v3-10"
	testClusterVerNS          = "cluster-operator"
	testClusterVerUID         = types.UID("test-cluster-version")
	testRegion                = "us-east-1"
	testImage                 = "testAMI"
	testVPCID                 = "testVPCID"
	testSubnetID              = "testSubnetID"
	testAZ                    = "us-east-1c"
	testMachineName           = "testmachine"
)

func TestUserDataTemplate(t *testing.T) {
	cases := []struct {
		name                string
		isMaster            bool
		isInfra             bool
		bootstrapKubeconfig string
		expectedUserData    string
	}{
		{
			name:     "master",
			isMaster: true,
			isInfra:  false,
			expectedUserData: `#cloud-config
write_files:
- path: /root/openshift_bootstrap/openshift_settings.yaml
  owner: 'root:root'
  permissions: '0640'
  content: |
    openshift_node_config_name: node-config-master
runcmd:
- [ ansible-playbook, /root/openshift_bootstrap/bootstrap.yml]`,
		},
		{
			name:     "master and infra",
			isMaster: true,
			isInfra:  true,
			expectedUserData: `#cloud-config
write_files:
- path: /root/openshift_bootstrap/openshift_settings.yaml
  owner: 'root:root'
  permissions: '0640'
  content: |
    openshift_node_config_name: node-config-master
runcmd:
- [ ansible-playbook, /root/openshift_bootstrap/bootstrap.yml]`,
		},
		{
			name:                "compute node",
			isMaster:            false,
			isInfra:             false,
			bootstrapKubeconfig: "testkubeconfig",
			expectedUserData: `#cloud-config
write_files:
- path: /root/openshift_bootstrap/openshift_settings.yaml
  owner: 'root:root'
  permissions: '0640'
  content: |
    openshift_node_config_name: node-config-compute
- path: /etc/origin/node/bootstrap.kubeconfig
  owner: 'root:root'
  permissions: '0640'
  encoding: b64
  content: testkubeconfig
runcmd:
- [ ansible-playbook, /root/openshift_bootstrap/bootstrap.yml]
- [ systemctl, restart, systemd-hostnamed]
- [ systemctl, restart, NetworkManager]
- [ systemctl, enable, origin-node]
- [ systemctl, start, origin-node]`,
		},
		{
			name:                "infra node",
			isMaster:            false,
			isInfra:             true,
			bootstrapKubeconfig: "testkubeconfig",
			expectedUserData: `#cloud-config
write_files:
- path: /root/openshift_bootstrap/openshift_settings.yaml
  owner: 'root:root'
  permissions: '0640'
  content: |
    openshift_node_config_name: node-config-infra
- path: /etc/origin/node/bootstrap.kubeconfig
  owner: 'root:root'
  permissions: '0640'
  encoding: b64
  content: testkubeconfig
runcmd:
- [ ansible-playbook, /root/openshift_bootstrap/bootstrap.yml]
- [ systemctl, restart, systemd-hostnamed]
- [ systemctl, restart, NetworkManager]
- [ systemctl, enable, origin-node]
- [ systemctl, start, origin-node]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsedTemplate, err := executeTemplate(tc.isMaster, tc.isInfra, tc.bootstrapKubeconfig)
			if assert.NoError(t, err) {
				assert.Equal(t, tc.expectedUserData, parsedTemplate)
			}
		})
	}

}

func TestBuildDescribeSecurityGroupsInput(t *testing.T) {
	cases := []struct {
		name               string
		isMaster           bool
		isInfra            bool
		expectedGroupNames []string
	}{
		{
			name:               "master",
			isMaster:           true,
			expectedGroupNames: []string{"cn", "cn_master", "cn_master_k8s"},
		},
		{
			name:               "master and infra",
			isMaster:           true,
			isInfra:            true,
			expectedGroupNames: []string{"cn", "cn_master", "cn_master_k8s", "cn_infra", "cn_infra_k8s"},
		},
		{
			name:               "infra",
			isInfra:            true,
			expectedGroupNames: []string{"cn", "cn_infra", "cn_infra_k8s"},
		},
		{
			name:               "compute",
			expectedGroupNames: []string{"cn", "cn_compute", "cn_compute_k8s"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := buildDescribeSecurityGroupsInput(testVPCID, "cn", tc.isMaster, tc.isInfra)
			assert.Equal(t, 2, len(input.Filters))
			groupNamesFilter, err := findFilter(input.Filters, "group-name")
			if assert.NoError(t, err) {
				groupNames := make([]string, 0, len(groupNamesFilter.Values))
				for _, sgn := range groupNamesFilter.Values {
					groupNames = append(groupNames, *sgn)
				}
				assert.ElementsMatch(t, groupNames, tc.expectedGroupNames)
			}

			vpcFilter, err := findFilter(input.Filters, "vpc-id")
			if assert.NoError(t, err) {
				assert.Equal(t, testVPCID, *vpcFilter.Values[0])
			}
		})
	}
}

func findFilter(filters []*ec2.Filter, name string) (*ec2.Filter, error) {
	for _, f := range filters {
		if *f.Name == name {
			return f, nil
		}
	}
	return nil, fmt.Errorf("unable to find filter: %s", name)
}

func TestCreateMachine(t *testing.T) {
	cases := []struct {
		name      string
		nodeType  clustopv1.NodeType
		isInfra   bool
		instances []*ec2.Instance
	}{
		{
			name:      "master",
			nodeType:  clustopv1.NodeTypeMaster,
			isInfra:   false,
			instances: []*ec2.Instance{},
		},
		{
			name:     "master w/ stopped instance",
			nodeType: clustopv1.NodeTypeMaster,
			isInfra:  false,
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "stopped", testClusterID, 30*time.Minute),
			},
		},
		{
			name:     "master w/ multiple stopped instances",
			nodeType: clustopv1.NodeTypeMaster,
			isInfra:  false,
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "stopped", testClusterID, 30*time.Minute),
				testInstance("i2", testMachineName, "master", "stopped", testClusterID, 30*time.Minute),
			},
		},
		{
			name:      "master and infra",
			nodeType:  clustopv1.NodeTypeMaster,
			isInfra:   true,
			instances: []*ec2.Instance{},
		},
		{
			name:      "infra node",
			nodeType:  clustopv1.NodeTypeCompute,
			isInfra:   true,
			instances: []*ec2.Instance{},
		},
		{
			name:     "infra node w/ stopped instance",
			nodeType: clustopv1.NodeTypeCompute,
			isInfra:  true,
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "node", "stopped", testClusterID, 30*time.Minute),
			},
		},
		{
			name:     "infra node w/ multiple stopped instances",
			nodeType: clustopv1.NodeTypeCompute,
			isInfra:  true,
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "node", "stopped", testClusterID, 30*time.Minute),
				testInstance("i2", testMachineName, "node", "stopped", testClusterID, 30*time.Minute),
			},
		},
		{
			name:      "compute node",
			nodeType:  clustopv1.NodeTypeCompute,
			isInfra:   false,
			instances: []*ec2.Instance{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			kubeClient := &clientgofake.Clientset{}
			capiClient := &capiclientfake.Clientset{}

			isMaster := tc.nodeType == clustopv1.NodeTypeMaster

			cluster, err := testCluster(t)
			if !assert.NoError(t, err) {
				return
			}
			machine := testMachine(testMachineName, cluster.Name, tc.nodeType, tc.isInfra, nil)

			mockAWSClient := mockaws.NewMockClient(mockCtrl)
			addDescribeImagesMock(mockAWSClient, testImage)
			addDescribeVpcsMock(mockAWSClient, testClusterID, testVPCID)
			addDescribeSubnetsMock(mockAWSClient, testAZ, testVPCID, testSubnetID)
			addDescribeSecurityGroupsMock(t, mockAWSClient, testVPCID, testClusterID, isMaster, tc.isInfra)

			if !isMaster {
				addDescribeInstancesMock(mockAWSClient, tc.instances)

				if len(tc.instances) > 0 {
					mockAWSClient.EXPECT().TerminateInstances(gomock.Any()).Do(func(input interface{}) {
						runInput, ok := input.(*ec2.TerminateInstancesInput)
						assert.True(t, ok)
						expectedTerminateIDs := []*string{}
						for _, i := range tc.instances {
							expectedTerminateIDs = append(expectedTerminateIDs, i.InstanceId)
						}
						assert.ElementsMatch(t, expectedTerminateIDs, runInput.InstanceIds)
					}).Return(&ec2.TerminateInstancesOutput{}, nil)
				}
			}

			mockAWSClient.EXPECT().RunInstances(gomock.Any()).Do(func(input interface{}) {
				runInput, ok := input.(*ec2.RunInstancesInput)
				assert.True(t, ok)
				assertRunInstancesInputHasTag(t, runInput, "clusterid", testClusterID)
				if isMaster {
					assertRunInstancesInputHasTag(t, runInput, "host-type", "master")
				} else {
					assertRunInstancesInputHasTag(t, runInput, "host-type", "node")
				}

				if tc.isInfra {
					assertRunInstancesInputHasTag(t, runInput, "sub-host-type", "infra")
				} else if isMaster {
					assertRunInstancesInputHasTag(t, runInput, "sub-host-type", "default")
				} else {
					assertRunInstancesInputHasTag(t, runInput, "sub-host-type", "compute")
				}

				assertRunInstancesInputHasTag(t, runInput, "kubernetes.io/cluster/"+testClusterID, testClusterID)
				assertRunInstancesInputHasTag(t, runInput, "Name", machine.Name)

				assert.Equal(t, testImage, *runInput.ImageId)
			}).Return(&ec2.Reservation{
				Instances: []*ec2.Instance{{InstanceId: aws.String("newinstance")}},
			}, nil)

			actuator := NewActuator(kubeClient, capiClient, log.WithField("test", "TestActuator"), "us-east-1c")
			actuator.clientBuilder = func(kubeClient kubernetes.Interface, mSpec *clustopv1.MachineSetSpec, namespace, region string) (Client, error) {
				return mockAWSClient, nil
			}
			actuator.userDataGenerator = func(master, infra bool) (string, error) {
				return "fakeuserdata", nil
			}

			instance, err := actuator.CreateMachine(cluster, machine)
			assert.NoError(t, err)
			assert.NotNil(t, instance)
		})
	}
}

func TestUpdate(t *testing.T) {
	cases := []struct {
		name               string
		instances          []*ec2.Instance
		currentStatus      *clustopv1.AWSMachineProviderStatus
		expectedInstanceID string
		// expectedUpdate should be true if we expect a cluster-api client update to be executed for changing machine status.
		expectedUpdate bool
		expectedError  bool
	}{
		{
			name: "one instance running no status change",
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "running", testClusterID, 30*time.Minute),
			},
			currentStatus:      testAWSStatus("i1"),
			expectedInstanceID: "i1",
			expectedUpdate:     false,
		},
		{
			name: "one instance running ip changed",
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "running", testClusterID, 30*time.Minute),
			},
			currentStatus: func() *clustopv1.AWSMachineProviderStatus {
				status := testAWSStatus("i1")
				status.PublicIP = aws.String("fake old IP")
				return status
			}(),
			expectedInstanceID: "i1",
			expectedUpdate:     true,
		},
		{
			name:               "instance deleted",
			instances:          []*ec2.Instance{},
			currentStatus:      testAWSStatus("i1"),
			expectedInstanceID: "",
			expectedUpdate:     true,
			expectedError:      true,
		},
		{
			name: "multiple instance running no status change",
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "running", testClusterID, 30*time.Minute),
				testInstance("i2", testMachineName, "master", "running", testClusterID, 120*time.Minute),
				testInstance("i3", testMachineName, "master", "running", testClusterID, 90*time.Minute),
				testInstance("i4", testMachineName, "master", "running", testClusterID, 5*time.Minute),
			},
			currentStatus:      testAWSStatus("i4"),
			expectedInstanceID: "i4",
			expectedUpdate:     false,
		},
		{
			name: "multiple instance running with status change",
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "running", testClusterID, 30*time.Minute),
				testInstance("i2", testMachineName, "master", "running", testClusterID, 120*time.Minute),
				testInstance("i3", testMachineName, "master", "running", testClusterID, 90*time.Minute),
				testInstance("i4", testMachineName, "master", "running", testClusterID, 5*time.Minute),
			},
			currentStatus:      testAWSStatus("i1"),
			expectedInstanceID: "i4",
			expectedUpdate:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			kubeClient := &clientgofake.Clientset{}
			capiClient := &capiclientfake.Clientset{}

			cluster, err := testCluster(t)
			if !assert.NoError(t, err) {
				return
			}
			machine := testMachine(testMachineName, cluster.Name, clustopv1.NodeTypeMaster, true, tc.currentStatus)

			mockAWSClient := mockaws.NewMockClient(mockCtrl)
			addDescribeInstancesMock(mockAWSClient, tc.instances)
			if len(tc.instances) > 1 {
				mockAWSClient.EXPECT().TerminateInstances(gomock.Any()).Do(func(input interface{}) {
					runInput, ok := input.(*ec2.TerminateInstancesInput)
					assert.True(t, ok)
					expectedTerminateIDs := []*string{}
					for _, i := range tc.instances {
						if *i.InstanceId != tc.expectedInstanceID {
							expectedTerminateIDs = append(expectedTerminateIDs, i.InstanceId)
						}
					}
					assert.ElementsMatch(t, expectedTerminateIDs, runInput.InstanceIds)
				}).Return(&ec2.TerminateInstancesOutput{}, nil)
			}

			actuator := NewActuator(kubeClient, capiClient, log.WithField("test", "TestActuator"), "us-east-1c")
			actuator.clientBuilder = func(kubeClient kubernetes.Interface, mSpec *clustopv1.MachineSetSpec, namespace, region string) (Client, error) {
				return mockAWSClient, nil
			}

			err = actuator.Update(cluster, machine)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tc.expectedUpdate {
				assert.Equal(t, 1, len(capiClient.Actions()))
				action := capiClient.Actions()[0]
				assert.Equal(t, "update", action.GetVerb())

				updateAction, ok := action.(clientgotesting.UpdateAction)
				assert.True(t, ok)

				updatedObject := updateAction.GetObject()
				machine, ok := updatedObject.(*capiv1.Machine)
				clustopStatus, err := controller.AWSMachineProviderStatusFromClusterAPIMachine(machine)
				if !assert.NoError(t, err) {
					return
				}
				if tc.expectedInstanceID == "" {
					assert.Nil(t, clustopStatus.InstanceID)
					assert.Nil(t, clustopStatus.PublicIP)
					assert.Nil(t, clustopStatus.PublicDNS)
					assert.Nil(t, clustopStatus.PrivateIP)
					assert.Nil(t, clustopStatus.PrivateDNS)
				} else {
					assert.Equal(t, tc.expectedInstanceID, *clustopStatus.InstanceID)
				}
				// LastELBSync should be cleared if our instance ID changed to trigger the ELB controller:
				if *tc.currentStatus.InstanceID != tc.expectedInstanceID {
					assert.Nil(t, clustopStatus.LastELBSync)
				} else {
					assert.NotNil(t, clustopStatus.LastELBSync)
				}
			} else {
				assert.Equal(t, 0, len(capiClient.Actions()))
			}
		})
	}
}

func TestDeleteMachine(t *testing.T) {
	cases := []struct {
		name      string
		instances []*ec2.Instance
	}{
		{
			name:      "no instances",
			instances: []*ec2.Instance{},
		},
		{
			name: "one instance running",
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "running", testClusterID, 30*time.Minute),
			},
		},
		{
			name: "multiple instances running",
			instances: []*ec2.Instance{
				testInstance("i1", testMachineName, "master", "running", testClusterID, 30*time.Minute),
				testInstance("i2", testMachineName, "master", "running", testClusterID, 120*time.Minute),
				testInstance("i3", testMachineName, "master", "running", testClusterID, 90*time.Minute),
				testInstance("i4", testMachineName, "master", "running", testClusterID, 5*time.Minute),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			kubeClient := &clientgofake.Clientset{}
			capiClient := &capiclientfake.Clientset{}

			cluster, err := testCluster(t)
			if !assert.NoError(t, err) {
				return
			}
			machine := testMachine(testMachineName, cluster.Name, clustopv1.NodeTypeMaster, true, nil)

			mockAWSClient := mockaws.NewMockClient(mockCtrl)
			addDescribeInstancesMock(mockAWSClient, tc.instances)
			if len(tc.instances) > 0 {
				mockAWSClient.EXPECT().TerminateInstances(gomock.Any()).Do(func(input interface{}) {
					runInput, ok := input.(*ec2.TerminateInstancesInput)
					assert.True(t, ok)
					expectedTerminateIDs := make([]*string, len(tc.instances))
					for i, instance := range tc.instances {
						expectedTerminateIDs[i] = instance.InstanceId
					}
					assert.ElementsMatch(t, expectedTerminateIDs, runInput.InstanceIds)
				}).Return(&ec2.TerminateInstancesOutput{}, nil)
			}

			actuator := NewActuator(kubeClient, capiClient, log.WithField("test", "TestActuator"), "us-east-1c")
			actuator.clientBuilder = func(kubeClient kubernetes.Interface, mSpec *clustopv1.MachineSetSpec, namespace, region string) (Client, error) {
				return mockAWSClient, nil
			}

			err = actuator.DeleteMachine(machine)
			assert.NoError(t, err)
		})
	}
}

func assertRunInstancesInputHasTag(t *testing.T, input *ec2.RunInstancesInput, key, value string) {
	for _, tag := range input.TagSpecifications[0].Tags {
		if *tag.Key == key {
			assert.Equal(t, *tag.Value, value, "bad value for instance tag: %s", key)
			return
		}
	}
	t.Errorf("tag not found on RunInstancesInput: %s", key)
}

func addDescribeImagesMock(mockAWSClient *mockaws.MockClient, imageID string) {
	mockAWSClient.EXPECT().DescribeImages(&ec2.DescribeImagesInput{
		ImageIds: []*string{aws.String(testImage)},
	}).Return(
		&ec2.DescribeImagesOutput{
			Images: []*ec2.Image{
				{
					ImageId: aws.String(testImage),
				},
			},
		}, nil)
}

func testInstance(instanceID, machineName, hostType, instanceState, clusterID string, age time.Duration) *ec2.Instance {
	tagList := []*ec2.Tag{
		{Key: aws.String("host-type"), Value: aws.String(hostType)},
		{Key: aws.String("sub-host-type"), Value: aws.String("default")},
		{Key: aws.String("kubernetes.io/cluster/" + clusterID), Value: aws.String(clusterID)},
		{Key: aws.String("clusterid"), Value: aws.String(clusterID)},
		{Key: aws.String("Name"), Value: aws.String(machineName)},
	}
	launchTime := time.Now().Add(-age)
	return &ec2.Instance{
		InstanceId:       &instanceID,
		Tags:             tagList,
		State:            &ec2.InstanceState{Name: &instanceState},
		PublicIpAddress:  aws.String(fmt.Sprintf("%s-publicip", instanceID)),
		PrivateIpAddress: aws.String(fmt.Sprintf("%s-privateip", instanceID)),
		PublicDnsName:    aws.String(fmt.Sprintf("%s-publicdns", instanceID)),
		PrivateDnsName:   aws.String(fmt.Sprintf("%s-privatednf", instanceID)),
		LaunchTime:       &launchTime,
	}
}

func testAWSStatus(instanceID string) *clustopv1.AWSMachineProviderStatus {
	return &clustopv1.AWSMachineProviderStatus{
		InstanceID:    aws.String(instanceID),
		InstanceState: aws.String("running"),
		// Match the assumptions made in testInstance based on instance ID.
		PublicIP:    aws.String(fmt.Sprintf("%s-publicip", instanceID)),
		PrivateIP:   aws.String(fmt.Sprintf("%s-privateip", instanceID)),
		PublicDNS:   aws.String(fmt.Sprintf("%s-publicdns", instanceID)),
		PrivateDNS:  aws.String(fmt.Sprintf("%s-privatednf", instanceID)),
		LastELBSync: &metav1.Time{Time: time.Now().Add(-30 * time.Minute)},
	}
}

func addDescribeInstancesMock(mockAWSClient *mockaws.MockClient, instances []*ec2.Instance) {
	// Wrap each instance in a reservation:
	reservations := make([]*ec2.Reservation, len(instances))
	for i, inst := range instances {
		reservations[i] = &ec2.Reservation{Instances: []*ec2.Instance{inst}}
	}

	mockAWSClient.EXPECT().DescribeInstances(gomock.Any()).Return(
		&ec2.DescribeInstancesOutput{
			Reservations: reservations,
		}, nil)
}

func addDescribeVpcsMock(mockAWSClient *mockaws.MockClient, vpcName, vpcID string) {
	describeVpcsInput := ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{{Name: aws.String("tag:Name"), Values: []*string{&vpcName}}},
	}
	describeVpcsOutput := ec2.DescribeVpcsOutput{
		Vpcs: []*ec2.Vpc{
			{
				VpcId: aws.String(vpcID),
			},
		},
	}
	mockAWSClient.EXPECT().DescribeVpcs(&describeVpcsInput).Return(&describeVpcsOutput, nil)
}

func addDescribeSubnetsMock(mockAWSClient *mockaws.MockClient, az, vpcID, subnetID string) {
	input := ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
			{Name: aws.String("availability-zone"), Values: []*string{aws.String(az)}},
		},
	}
	output := ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{
			{
				SubnetId: aws.String(subnetID),
			},
		},
	}
	mockAWSClient.EXPECT().DescribeSubnets(&input).Return(&output, nil)
}

func addDescribeSecurityGroupsMock(t *testing.T, mockAWSClient *mockaws.MockClient, vpcID, vpcName string, isMaster, isInfra bool) {
	// Using the input builder from the production code. Behavior of this function is tested separately.
	input := buildDescribeSecurityGroupsInput(vpcID, vpcName, isMaster, isInfra)
	output := ec2.DescribeSecurityGroupsOutput{
		SecurityGroups: []*ec2.SecurityGroup{},
	}
	// Add a fake ID for each security group we're searching for.
	sgFilter, err := findFilter(input.Filters, "group-name")
	if assert.NoError(t, err) {
		for _, groupName := range sgFilter.Values {
			output.SecurityGroups = append(output.SecurityGroups, &ec2.SecurityGroup{GroupId: aws.String(fmt.Sprintf("%s-ID", *groupName))})
		}
		mockAWSClient.EXPECT().DescribeSecurityGroups(input).Return(&output, nil)
	}
}

// testClusterDeployment creates a new test ClusterDeployment
func testClusterDeployment() *clustopv1.ClusterDeployment {
	clusterDeployment := &clustopv1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterDeploymentUUID,
			Name:      testClusterDeploymentName,
			Namespace: testNamespace,
		},
		Spec: clustopv1.ClusterDeploymentSpec{
			ClusterID: testClusterID,
			MachineSets: []clustopv1.ClusterMachineSet{
				{
					ShortName: "",
					MachineSetConfig: clustopv1.MachineSetConfig{
						Infra:    true,
						Size:     1,
						NodeType: clustopv1.NodeTypeMaster,
					},
				},
				{
					ShortName: "compute",
					MachineSetConfig: clustopv1.MachineSetConfig{
						Infra:    false,
						Size:     1,
						NodeType: clustopv1.NodeTypeCompute,
					},
				},
			},
			Hardware: clustopv1.ClusterHardwareSpec{
				AWS: &clustopv1.AWSClusterSpec{
					SSHUser:     "clusteroperator",
					Region:      testRegion,
					KeyPairName: "libra",
				},
			},
			ClusterVersionRef: clustopv1.ClusterVersionReference{
				Name:      testClusterVerName,
				Namespace: testClusterVerNS,
			},
		},
	}
	return clusterDeployment
}

func testCluster(t *testing.T) (*capiv1.Cluster, error) {
	clusterDeployment := testClusterDeployment()
	return controller.BuildCluster(clusterDeployment, testClusterVersion())
}

func testMachine(name, clusterName string, nodeType clustopv1.NodeType, isInfra bool, currentStatus *clustopv1.AWSMachineProviderStatus) *capiv1.Machine {
	testAMI := testImage
	msSpec := clustopv1.MachineSetSpec{
		ClusterID: testClusterID,
		MachineSetConfig: clustopv1.MachineSetConfig{
			Infra:    isInfra,
			Size:     3,
			NodeType: nodeType,
			Hardware: &clustopv1.MachineSetHardwareSpec{
				AWS: &clustopv1.MachineSetAWSHardwareSpec{
					InstanceType: "t2.micro",
				},
			},
		},
		ClusterHardware: clustopv1.ClusterHardwareSpec{
			AWS: &clustopv1.AWSClusterSpec{
				Region: testRegion,
			},
		},
		VMImage: clustopv1.VMImage{
			AWSImage: &testAMI,
		},
	}
	rawProviderConfig, _ := controller.MachineProviderConfigFromMachineSetSpec(&msSpec)
	machine := &capiv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				clustopv1.ClusterNameLabel: clusterName,
			},
		},
		Spec: capiv1.MachineSpec{
			ProviderConfig: capiv1.ProviderConfig{
				Value: rawProviderConfig,
			},
		},
	}
	if nodeType == clustopv1.NodeTypeMaster {
		machine.Spec.Roles = []capicommon.MachineRole{capicommon.MasterRole}
	} else {
		machine.Spec.Roles = []capicommon.MachineRole{capicommon.NodeRole}
	}
	if currentStatus != nil {
		rawStatus, _ := controller.EncodeAWSMachineProviderStatus(currentStatus)
		machine.Status.ProviderStatus = rawStatus
	}
	return machine
}

func testClusterVersion() clustopv1.ClusterVersionSpec {
	masterAMI := "master-AMI-west"
	return clustopv1.ClusterVersionSpec{
		Images: clustopv1.ClusterVersionImages{
			ImageFormat: "openshift/origin-${component}:v3.10.0",
		},
		VMImages: clustopv1.VMImages{
			AWSImages: &clustopv1.AWSVMImages{
				RegionAMIs: []clustopv1.AWSRegionAMIs{
					{
						Region: "us-east-1",
						AMI:    "compute-AMI-east",
					},
					{
						Region:    "us-west-1",
						AMI:       "compute-AMI-west",
						MasterAMI: &masterAMI,
					},
				},
			},
		},
		DeploymentType: clustopv1.ClusterDeploymentTypeOrigin,
		Version:        "v3.10.0",
	}
}
