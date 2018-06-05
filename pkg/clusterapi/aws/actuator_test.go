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

	"github.com/golang/mock/gomock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	clustopv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	"github.com/openshift/cluster-operator/pkg/controller"
	"github.com/openshift/cluster-operator/pkg/controller/clusterdeployment"

	capicommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientfake "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgofake "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace             = "test-namespace"
	testClusterDeploymentName = "test-cluster-deployment"
	testClusterDeploymentUUID = types.UID("test-cluster-deployment-uuid")
	testClusterVerName        = "v3-10"
	testClusterVerNS          = "cluster-operator"
	testClusterVerUID         = types.UID("test-cluster-version")
	testRegion                = "us-east-1"
	testImage                 = "testAMI"
	testVPCID                 = "testVPCID"
	testSubnetID              = "testSubnetID"
	testAZ                    = "us-east-1c"
)

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
		name     string
		nodeType clustopv1.NodeType
		isInfra  bool
	}{
		{
			name:     "master",
			nodeType: clustopv1.NodeTypeMaster,
			isInfra:  false,
		},
		{
			name:     "master and infra",
			nodeType: clustopv1.NodeTypeMaster,
			isInfra:  true,
		},
		{
			name:     "infra node",
			nodeType: clustopv1.NodeTypeCompute,
			isInfra:  true,
		},
		{
			name:     "compute node",
			nodeType: clustopv1.NodeTypeCompute,
			isInfra:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			kubeClient := &clientgofake.Clientset{}
			capiClient := &capiclientfake.Clientset{}

			isMaster := tc.nodeType == clustopv1.NodeTypeMaster

			cluster := testCluster(t)
			machine := testMachine("testmachine", tc.nodeType, tc.isInfra)

			mockAWSClient := NewMockClient(mockCtrl)
			addDescribeImagesMock(mockAWSClient, testImage)
			addDescribeVpcsMock(mockAWSClient, cluster.Name, testVPCID)
			addDescribeSubnetsMock(mockAWSClient, testAZ, testVPCID, testSubnetID)
			addDescribeSecurityGroupsMock(t, mockAWSClient, testVPCID, cluster.Name, isMaster, tc.isInfra)
			mockAWSClient.EXPECT().RunInstances(gomock.Any()).Do(func(input interface{}) {
				runInput, ok := input.(*ec2.RunInstancesInput)
				assert.True(t, ok)
				assertRunInstancesInputHasTag(t, runInput, "clusterid", cluster.Name)
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

				assertRunInstancesInputHasTag(t, runInput, "kubernetes.io/cluster/"+cluster.Name, cluster.Name)
				assertRunInstancesInputHasTag(t, runInput, "Name", machine.Name)

				assert.Equal(t, testImage, *runInput.ImageId)
			}).Return(&ec2.Reservation{
				Instances: []*ec2.Instance{{InstanceId: aws.String("newinstance")}},
			}, nil)

			actuator := NewActuator(kubeClient, capiClient, log.WithField("test", "TestActuator"), "us-east-1c")
			actuator.clientBuilder = func(kubeClient kubernetes.Interface, mSpec *clustopv1.MachineSetSpec, namespace, region string) (Client, error) {
				return mockAWSClient, nil
			}

			instance, err := actuator.CreateMachine(cluster, machine)
			assert.NoError(t, err)
			assert.NotNil(t, instance)
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

func addDescribeImagesMock(mockAWSClient *MockClient, imageID string) {
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

func addDescribeVpcsMock(mockAWSClient *MockClient, vpcName, vpcID string) {
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

func addDescribeSubnetsMock(mockAWSClient *MockClient, az, vpcID, subnetID string) {
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

func addDescribeSecurityGroupsMock(t *testing.T, mockAWSClient *MockClient, vpcID, vpcName string, isMaster, isInfra bool) {
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

func testCluster(t *testing.T) *capiv1.Cluster {
	clusterDeployment := testClusterDeployment()
	cluster, err := clusterdeployment.BuildCluster(clusterDeployment)
	assert.NoError(t, err)
	return cluster
}

func testMachine(name string, nodeType clustopv1.NodeType, isInfra bool) *capiv1.Machine {
	testAMI := testImage
	msSpec := clustopv1.MachineSetSpec{
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
	rawProviderConfig, _ := controller.ClusterAPIMachineProviderConfigFromMachineSetSpec(&msSpec)
	machine := &capiv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
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
	return machine
}
