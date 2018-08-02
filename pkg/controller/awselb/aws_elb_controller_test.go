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

package awselb

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elb"

	clustopv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	clustopclientfake "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	clustopaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws"
	mockaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws/mock"
	"github.com/openshift/cluster-operator/pkg/controller"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclientfake "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"
	capiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"

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
	testMachineName           = "testmachine"
	testNamespace             = "test-namespace"
	testClusterDeploymentName = "test-cluster-deployment"
	testClusterDeploymentUUID = types.UID("test-cluster-deployment-uuid")
	testClusterName           = "test-cluster-name"
	testRegion                = "us-east-1"
	testClusterVerName        = "v3-10"
	testClusterVerNS          = "cluster-operator"
	testClusterVerUID         = types.UID("test-cluster-version")
	testImage                 = "testAMI"
)

func TestSyncMachine(t *testing.T) {
	cases := []struct {
		name                         string
		generation                   int64
		lastSuccessfulSyncGeneration int64
		lastSuccessfulSync           time.Time
		resyncExpected               bool
	}{
		{
			name:                         "no last sync",
			generation:                   1,
			lastSuccessfulSyncGeneration: 1,
			resyncExpected:               true,
		},
		{
			name:                         "no re-sync required",
			lastSuccessfulSync:           time.Now().Add(-5 * time.Second),
			generation:                   1,
			lastSuccessfulSyncGeneration: 1,
			resyncExpected:               false,
		},
		{
			name:                         "re-sync due",
			lastSuccessfulSync:           time.Now().Add(-9 * time.Hour),
			generation:                   1,
			lastSuccessfulSyncGeneration: 1,
			resyncExpected:               true,
		},
		{
			name:                         "re-sync for generation change",
			lastSuccessfulSync:           time.Now().Add(-5 * time.Second),
			generation:                   4,
			lastSuccessfulSyncGeneration: 1,
			resyncExpected:               true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			kubeClient := &clientgofake.Clientset{}
			clustopClient := &clustopclientfake.Clientset{}
			capiClient := &capiclientfake.Clientset{}
			capiInformers := capiinformers.NewSharedInformerFactory(capiClient, 0)

			status := clustopv1.AWSMachineProviderStatus{}
			status.InstanceID = aws.String("preserveme")
			if !tc.lastSuccessfulSync.IsZero() {
				status.LastELBSync = &metav1.Time{Time: tc.lastSuccessfulSync}
			}
			status.LastELBSyncGeneration = tc.lastSuccessfulSyncGeneration
			cluster := testCluster(t)
			machine := testMachine(tc.generation, testMachineName, cluster.Name, clustopv1.NodeTypeMaster, true, &status)

			mockAWSClient := mockaws.NewMockClient(mockCtrl)
			if tc.resyncExpected {
				mockTestInstance(mockAWSClient, "i1", machine.Name, testClusterName)
				mockRegisterInstancesWithELB(mockAWSClient, "i1", controller.ELBMasterExternalName(testClusterName))
				mockRegisterInstancesWithELB(mockAWSClient, "i1", controller.ELBMasterInternalName(testClusterName))
			}

			ctrlr := NewController(capiInformers.Cluster().V1alpha1().Machines(),
				kubeClient, clustopClient, capiClient)
			ctrlr.clientBuilder = func(kubeClient kubernetes.Interface, mSpec *clustopv1.MachineSetSpec, namespace, region string) (clustopaws.Client, error) {
				return mockAWSClient, nil
			}

			err := ctrlr.processMachine(machine)
			assert.NoError(t, err)

			if tc.resyncExpected {
				if !assert.Equal(t, 1, len(capiClient.Actions())) {
					return
				}
				action := capiClient.Actions()[0]
				assert.Equal(t, "update", action.GetVerb())

				updateAction, ok := action.(clientgotesting.UpdateAction)
				assert.True(t, ok)

				updatedObject := updateAction.GetObject()
				machine, ok := updatedObject.(*capiv1.Machine)
				clustopStatus, err := controller.AWSMachineProviderStatusFromClusterAPIMachine(machine)
				assert.NoError(t, err)
				assert.NotNil(t, clustopStatus.LastELBSync)
				assert.True(t, clustopStatus.LastELBSync.Time.After(tc.lastSuccessfulSync))
				assert.Equal(t, machine.Generation, clustopStatus.LastELBSyncGeneration)
				assert.Equal(t, "preserveme", *clustopStatus.InstanceID)
			} else {
				assert.Equal(t, 0, len(capiClient.Actions()))
			}
		})
	}
}

func mockRegisterInstancesWithELB(mockAWSClient *mockaws.MockClient, instanceID, elbName string) {
	input := elb.RegisterInstancesWithLoadBalancerInput{
		Instances:        []*elb.Instance{{InstanceId: &instanceID}},
		LoadBalancerName: &elbName,
	}
	mockAWSClient.EXPECT().RegisterInstancesWithLoadBalancer(&input).Return(
		&elb.RegisterInstancesWithLoadBalancerOutput{}, nil)
}

func mockTestInstance(mockAWSClient *mockaws.MockClient, instanceID, machineName, clusterID string) {
	tagList := []*ec2.Tag{
		{Key: aws.String("host-type"), Value: aws.String("master")},
		{Key: aws.String("sub-host-type"), Value: aws.String("default")},
		{Key: aws.String("kubernetes.io/cluster/" + clusterID), Value: aws.String(clusterID)},
		{Key: aws.String("clusterid"), Value: aws.String(clusterID)},
		{Key: aws.String("Name"), Value: aws.String(machineName)},
	}
	launchTime := time.Now().Add(-60 * time.Hour)
	instance := &ec2.Instance{
		InstanceId:       &instanceID,
		Tags:             tagList,
		State:            &ec2.InstanceState{Name: aws.String("running")},
		PublicIpAddress:  aws.String(fmt.Sprintf("%s-publicip", instanceID)),
		PrivateIpAddress: aws.String(fmt.Sprintf("%s-privateip", instanceID)),
		PublicDnsName:    aws.String(fmt.Sprintf("%s-publicdns", instanceID)),
		PrivateDnsName:   aws.String(fmt.Sprintf("%s-privatednf", instanceID)),
		LaunchTime:       &launchTime,
	}

	reservations := []*ec2.Reservation{{Instances: []*ec2.Instance{instance}}}
	mockAWSClient.EXPECT().DescribeInstances(gomock.Any()).Return(
		&ec2.DescribeInstancesOutput{
			Reservations: reservations,
		}, nil)
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
			ClusterName: testClusterName,
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
	cluster, err := controller.BuildCluster(clusterDeployment, testClusterVersion().Spec)
	assert.NoError(t, err)
	return cluster
}

// testClusterVersion will create a ClusterVersion resource.
func testClusterVersion() *clustopv1.ClusterVersion {
	cv := &clustopv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			UID:       testClusterVerUID,
			Name:      testClusterVerName,
			Namespace: testClusterVerNS,
		},
		Spec: clustopv1.ClusterVersionSpec{
			Images: clustopv1.ClusterVersionImages{
				ImageFormat: "openshift/origin-${component}:${version}",
			},
			VMImages: clustopv1.VMImages{
				AWSImages: &clustopv1.AWSVMImages{
					RegionAMIs: []clustopv1.AWSRegionAMIs{
						{
							Region: testRegion,
							AMI:    "computeAMI_ID",
						},
					},
				},
			},
		},
	}
	return cv
}

func testMachine(generation int64, name, clusterName string, nodeType clustopv1.NodeType, isInfra bool, currentStatus *clustopv1.AWSMachineProviderStatus) *capiv1.Machine {
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
	rawProviderConfig, _ := controller.MachineProviderConfigFromMachineSetSpec(&msSpec)
	machine := &capiv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testNamespace,
			Generation: generation,
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
	if currentStatus != nil {
		rawStatus, _ := controller.EncodeAWSMachineProviderStatus(currentStatus)
		machine.Status.ProviderStatus = rawStatus
	}
	return machine
}
