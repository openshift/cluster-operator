/*
Copyright 2017 The Kubernetes Authors.

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

package registryinfra

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"

	kv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	kschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"

	clusteropclientfake "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset/fake"
	clusteropinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions"

	clusterapiclientfake "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/fake"
	clusterapiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"

	clusterapiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	clusteroperatorv1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	"github.com/openshift/cluster-operator/pkg/controller"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/golang/mock/gomock"
	clustopaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws"
	mockaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws/mock"
)

const (
	testRegion                      = "us-east-1"
	testClusterVerUID               = types.UID("test-cluster-version")
	testClusterVerName              = "v3-9"
	testClusterVerNS                = "cluster-operator"
	testClusterUUID                 = types.UID("test-cluster-uuid")
	testClusterDeploymentName       = "testcluster"
	testClusterDeploymentGeneration = 1
	testClusterName                 = "testcluster-id"
	testClusterNamespace            = "testsyncns"
	testAccessKeyID                 = "MYTESTACCESSKEY"
	testSecretAccessKey             = "MYTESTSECRETACCESSKEY"
)

type expectedAction struct {
	namespace string
	verb      string
	gvr       kschema.GroupVersionResource
	validate  func(t *testing.T, action clientgotesting.Action)
}

type testContext struct {
	controller     *Controller
	clusterStore   cache.Store
	fakeKubeClient *clientgofake.Clientset
}

// Sets up all of the mocks, informers, cache, etc for tests.
func setupTest() *testContext {
	capiClient := &clusterapiclientfake.Clientset{}
	clusteropClient := &clusteropclientfake.Clientset{}
	kubeClient := &clientgofake.Clientset{}

	clusterapiInformers := clusterapiinformers.NewSharedInformerFactory(capiClient, 0)
	clusteropInformers := clusteropinformers.NewSharedInformerFactory(clusteropClient, 0)

	ctx := &testContext{
		controller: NewController(
			clusterapiInformers.Cluster().V1alpha1().Clusters(),
			clusteropInformers.Clusteroperator().V1alpha1().ClusterDeployments(),
			kubeClient,
			clusteropClient,
			capiClient,
		),
		clusterStore:   clusterapiInformers.Cluster().V1alpha1().Clusters().Informer().GetStore(),
		fakeKubeClient: kubeClient,
	}

	return ctx
}

func newClusterVer(namespace, name string, uid types.UID) *clusteroperatorv1.ClusterVersion {
	cv := &clusteroperatorv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			UID:       uid,
			Name:      name,
			Namespace: namespace,
		},
		Spec: clusteroperatorv1.ClusterVersionSpec{
			Images: clusteroperatorv1.ClusterVersionImages{
				ImageFormat: "openshift/origin-${component}:${version}",
			},
			VMImages: clusteroperatorv1.VMImages{
				AWSImages: &clusteroperatorv1.AWSVMImages{
					RegionAMIs: []clusteroperatorv1.AWSRegionAMIs{
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
func newClusterFromClusterDeployment(t *testing.T, clusterDeployment *clusteroperatorv1.ClusterDeployment) *clusterapiv1.Cluster {
	cluster := &clusterapiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterDeployment.Spec.ClusterName,
			Namespace:  clusterDeployment.Namespace,
			Generation: clusterDeployment.Generation,
		},
		Spec: clusterapiv1.ClusterSpec{},
	}

	cv := newClusterVer(testClusterVerNS, testClusterVerName, testClusterVerUID)
	providerConfig, err := controller.BuildAWSClusterProviderConfig(&clusterDeployment.Spec, cv.Spec)
	if err != nil {
		t.Fatalf("error getting provider config from clusterdeployment: %v", err)
	}
	cluster.Spec.ProviderConfig.Value = providerConfig

	controller.AddFinalizer(cluster, clusteroperatorv1.FinalizerRegistryInfra)

	return cluster
}

func newClusterDeployment() *clusteroperatorv1.ClusterDeployment {
	clusterDeployment := &clusteroperatorv1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testClusterDeploymentName,
			Namespace:  testClusterNamespace,
			UID:        testClusterUUID,
			Generation: testClusterDeploymentGeneration,
		},
		Spec: clusteroperatorv1.ClusterDeploymentSpec{
			ClusterName: testClusterName,
			Hardware: clusteroperatorv1.ClusterHardwareSpec{
				AWS: &clusteroperatorv1.AWSClusterSpec{
					Region: testRegion,
				},
			},
		},
	}

	return clusterDeployment
}

func TestCreateInfra(t *testing.T) {
	cases := []struct {
		name              string
		clusterDeployment *clusteroperatorv1.ClusterDeployment
		existingBucket    bool
		existingIAMUser   bool
		setupS3           func(*mockaws.MockClient, *clusterapiv1.Cluster)
		setupIamUser      func(*mockaws.MockClient, *clusterapiv1.Cluster, *testContext, *testing.T)

		expectedActions   []expectedAction
		unexpectedActions []expectedAction
	}{
		{
			name:              "everything already exists",
			clusterDeployment: newClusterDeployment(),
			existingIAMUser:   true,
			setupS3:           mockS3ProvisionExistingBucket,
			setupIamUser:      mockIAMProvisionExistingUser,
			unexpectedActions: []expectedAction{
				{
					namespace: testClusterNamespace,
					verb:      "create",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
			},
		},
		{
			name:              "nothing exists",
			clusterDeployment: newClusterDeployment(),
			existingIAMUser:   false,
			setupS3:           mockS3ProvisionNoExistingBucket,
			setupIamUser:      mockIAMProvisionNoExistingUser,
			expectedActions: []expectedAction{
				{
					namespace: testClusterNamespace,
					verb:      "get",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
				{
					namespace: testClusterNamespace,
					verb:      "create",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
			},
		},
	}

	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := setupTest()

			// get visibility into logger
			tLog := ctx.controller.logger

			// create cluster objects
			cluster := newClusterFromClusterDeployment(t, tc.clusterDeployment)
			ctx.clusterStore.Add(cluster)

			// mock presense of existing secret holding iam creds if user already exists
			if tc.existingIAMUser {
				//
				ctx.fakeKubeClient.AddReactor("get", "secrets", func(action clientgotesting.Action) (bool, kruntime.Object, error) {
					return true, nil, nil
				})
			} else {
				// return notFound when searching for the secret
				ctx.fakeKubeClient.AddReactor("get", "secrets", func(action clientgotesting.Action) (bool, kruntime.Object, error) {
					return true, nil, errors.NewNotFound(kschema.GroupResource{}, "")
				})
			}

			// set up cloud
			mockCtrl := gomock.NewController(t)
			mockAWSClient := mockaws.NewMockClient(mockCtrl)

			tc.setupS3(mockAWSClient, cluster)
			tc.setupIamUser(mockAWSClient, cluster, ctx, t)

			ctx.controller.awsClientBuilder = func(kubeClient kubernetes.Interface, secretName, namespace, region string) (clustopaws.Client, error) {
				return mockAWSClient, nil
			}
			// Act
			err := ctx.controller.syncCluster(getKey(cluster, t))

			// Assert
			assert.NoError(t, err)

			validateExpectedActions(t, tLog, ctx.fakeKubeClient.Actions(), tc.expectedActions)
			validateUnexpectedActions(t, tLog, ctx.fakeKubeClient.Actions(), tc.unexpectedActions)
		})
	}
}

func TestDeleteInfra(t *testing.T) {
	cases := []struct {
		name              string
		clusterDeployment *clusteroperatorv1.ClusterDeployment
		setupS3           func(*mockaws.MockClient, *clusterapiv1.Cluster, *testContext)
		setupIAMUser      func(*mockaws.MockClient, *clusterapiv1.Cluster, *testContext, *testing.T)
		expectedActions   []expectedAction
		unexpectedActions []expectedAction
	}{
		{
			name:              "need to delete everything",
			clusterDeployment: newClusterDeployment(),
			setupS3:           mockS3DeprovisionExisitingBucket,
			setupIAMUser:      mockIAMDeprovisionExistingUser,
			unexpectedActions: []expectedAction{
				{
					namespace: testClusterNamespace,
					verb:      "create",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
			},
			expectedActions: []expectedAction{
				{
					namespace: testClusterNamespace,
					verb:      "get",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
				{
					namespace: testClusterNamespace,
					verb:      "delete",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
			},
		},
		{
			name:              "nothing exists",
			clusterDeployment: newClusterDeployment(),
			setupS3:           mockS3DeprovisionNoExistingBucket,
			setupIAMUser:      mockIAMDeprovisionNoExistingUser,
			unexpectedActions: []expectedAction{
				{
					namespace: testClusterNamespace,
					verb:      "delete",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
			},
			expectedActions: []expectedAction{
				{
					namespace: testClusterNamespace,
					verb:      "get",
					gvr:       kv1.SchemeGroupVersion.WithResource("secrets"),
				},
			},
		},
	}

	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := setupTest()

			// get visibility into logger
			tLog := ctx.controller.logger

			// create cluster objects
			cluster := newClusterFromClusterDeployment(t, tc.clusterDeployment)
			deletionTime := &metav1.Time{
				Time: time.Now(),
			}
			cluster.DeletionTimestamp = deletionTime
			ctx.clusterStore.Add(cluster)

			// set up cloud client
			mockCtrl := gomock.NewController(t)
			mockAWSClient := mockaws.NewMockClient(mockCtrl)

			tc.setupS3(mockAWSClient, cluster, ctx)
			tc.setupIAMUser(mockAWSClient, cluster, ctx, t)

			ctx.controller.awsClientBuilder = func(kubeClient kubernetes.Interface, secretName, namespace, region string) (clustopaws.Client, error) {
				return mockAWSClient, nil
			}

			// Act
			err := ctx.controller.syncCluster(getKey(cluster, t))

			// Assert
			assert.NoError(t, err)

			validateExpectedActions(t, tLog, ctx.fakeKubeClient.Actions(), tc.expectedActions)
			validateUnexpectedActions(t, tLog, ctx.fakeKubeClient.Actions(), tc.unexpectedActions)
		})
	}
}

// set up S3 deprovision expectations when the S3 bucket already exists
func mockS3DeprovisionExisitingBucket(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster, ctx *testContext) {
	bucketName := cluster.Name + "-registry"
	listBucketOutput := &s3.ListBucketsOutput{}

	mockBucket := s3.Bucket{
		Name: aws.String(bucketName),
	}
	listBucketOutput.Buckets = []*s3.Bucket{
		&mockBucket,
	}

	mockAwsClient.EXPECT().ListBuckets(nil).Return(listBucketOutput, nil)

	ctx.controller.awsEmptyBucket = func(string, clustopaws.Client) error { return nil }

	deleteBucketInput := &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	}
	mockAwsClient.EXPECT().DeleteBucket(deleteBucketInput).Return(&s3.DeleteBucketOutput{}, nil)
}

// set up S3 deprovision expectations when the S3 bucket has already been deleted
func mockS3DeprovisionNoExistingBucket(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster, ctx *testContext) {
	listBucketOutput := &s3.ListBucketsOutput{}

	mockAwsClient.EXPECT().ListBuckets(nil).Return(listBucketOutput, nil)
}

// set up S3 provision expectations when the S3 bucket hasn't already been provisioned
func mockS3ProvisionNoExistingBucket(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster) {
	bucketName := cluster.Name + "-registry"
	listOutput := &s3.ListBucketsOutput{}

	// expect some creates
	createInput := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}
	createOutput := &s3.CreateBucketOutput{}

	mockAwsClient.EXPECT().ListBuckets(nil).Return(listOutput, nil)
	mockAwsClient.EXPECT().CreateBucket(createInput).Return(createOutput, nil)

}

// set up S3 provision expectations when the S3 bucket has already been provisioned
func mockS3ProvisionExistingBucket(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster) {
	bucketName := cluster.Name + "-registry"
	listOutput := &s3.ListBucketsOutput{}

	// no create call expected
	fakeBucket := s3.Bucket{
		Name: aws.String(bucketName),
	}
	listOutput.Buckets = []*s3.Bucket{
		&fakeBucket,
	}

	mockAwsClient.EXPECT().ListBuckets(nil).Return(listOutput, nil)
}

// set up IAM deprovision expectations when the IAM user has previously been provisioned
func mockIAMDeprovisionExistingUser(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster, ctx *testContext, t *testing.T) {
	userName := cluster.Name + "-registry"
	policyName := cluster.Name + "S3Access"

	// set up a fake secret holding the IAM creds
	ctx.fakeKubeClient.AddReactor("get", "secrets", func(action clientgotesting.Action) (bool, kruntime.Object, error) {
		return true, nil, nil
	})

	// always a GetUser to query for an existing user
	getUserInput := &iam.GetUserInput{
		UserName: aws.String(userName),
	}
	getUserOutput := &iam.GetUserOutput{}

	// return that there is an existing user
	getUserOutput.User = &iam.User{
		UserName: aws.String(userName),
	}
	mockAwsClient.EXPECT().GetUser(getUserInput).Return(getUserOutput, nil)

	// provide the policies attached to that user
	listUserPoliciesInput := &iam.ListUserPoliciesInput{
		UserName: aws.String(userName),
	}
	listUserPoliciesOutput := &iam.ListUserPoliciesOutput{
		PolicyNames: []*string{
			aws.String(policyName),
		},
	}
	mockAwsClient.EXPECT().ListUserPolicies(listUserPoliciesInput).Return(listUserPoliciesOutput, nil)

	// watch for deletion of the policies attached to the user
	deleteUserPolicyInput := &iam.DeleteUserPolicyInput{
		UserName:   aws.String(userName),
		PolicyName: aws.String(policyName),
	}
	mockAwsClient.EXPECT().DeleteUserPolicy(deleteUserPolicyInput).Return(&iam.DeleteUserPolicyOutput{}, nil)

	// provide list of access keys attached to user
	listAccessKeysInput := &iam.ListAccessKeysInput{
		UserName: aws.String(userName),
	}
	listAccessKeysOutput := &iam.ListAccessKeysOutput{
		AccessKeyMetadata: []*iam.AccessKeyMetadata{
			{
				AccessKeyId: aws.String(testAccessKeyID),
			},
		},
	}
	mockAwsClient.EXPECT().ListAccessKeys(listAccessKeysInput).Return(listAccessKeysOutput, nil)

	// catch the delete of the access key
	deleteAccessKeyInput := &iam.DeleteAccessKeyInput{
		UserName:    aws.String(userName),
		AccessKeyId: aws.String(testAccessKeyID),
	}
	mockAwsClient.EXPECT().DeleteAccessKey(deleteAccessKeyInput).Return(&iam.DeleteAccessKeyOutput{}, nil)

	// and finally the DeleteUser()
	deleteUserInput := &iam.DeleteUserInput{
		UserName: aws.String(userName),
	}
	mockAwsClient.EXPECT().DeleteUser(deleteUserInput).Return(&iam.DeleteUserOutput{}, nil)
}

// set up IAM deprovision expectations when the IAM user has been previously deprovisioned
func mockIAMDeprovisionNoExistingUser(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster, ctx *testContext, t *testing.T) {
	userName := cluster.Name + "-registry"

	// indicate that there is no secret holding the IAM creds
	ctx.fakeKubeClient.AddReactor("get", "secrets", func(action clientgotesting.Action) (bool, kruntime.Object, error) {
		return true, nil, errors.NewNotFound(kschema.GroupResource{}, "")
	})

	// always a GetUser to query for an existing user
	getUserInput := &iam.GetUserInput{
		UserName: aws.String(userName),
	}

	// user doesn't exist
	err := awserr.New(iam.ErrCodeNoSuchEntityException, "user not found", fmt.Errorf("user not found"))
	mockAwsClient.EXPECT().GetUser(getUserInput).Return(nil, err)
}

// set up IAM provision expectations when the user has already been provisioned
func mockIAMProvisionExistingUser(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster, ctx *testContext, t *testing.T) {
	bucketName := controller.RegistryObjectStoreName(cluster.Name)
	userName := cluster.Name + "-registry"
	policyName := cluster.Name + "S3Access"
	policyDocument, err := createS3IamPolicy(bucketName)
	if err != nil {
		t.Fatalf("error creating expected policy document: %v", err)
	}

	// always a GetUser to query for an existing user
	getUserInput := &iam.GetUserInput{
		UserName: aws.String(userName),
	}
	getUserOutput := &iam.GetUserOutput{
		User: &iam.User{
			UserName: aws.String(userName),
		},
	}
	mockAwsClient.EXPECT().GetUser(getUserInput).Return(getUserOutput, nil)

	// always a PutUserPolicy (even if user already exists)
	putUserPolicyInput := &iam.PutUserPolicyInput{
		UserName:       aws.String(userName),
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(policyDocument),
	}
	putUserPolicyOutput := &iam.PutUserPolicyOutput{}
	mockAwsClient.EXPECT().PutUserPolicy(putUserPolicyInput).Return(putUserPolicyOutput, nil)

}

// set up IAM provision expectations when the user hasn't yet been provisioned
func mockIAMProvisionNoExistingUser(mockAwsClient *mockaws.MockClient, cluster *clusterapiv1.Cluster, ctx *testContext, t *testing.T) {
	bucketName := controller.RegistryObjectStoreName(cluster.Name)
	userName := cluster.Name + "-registry"
	policyName := cluster.Name + "S3Access"
	policyDocument, err := createS3IamPolicy(bucketName)
	if err != nil {
		t.Fatalf("error creating expected policy document: %v", err)
	}

	// always a GetUser to query for an existing user
	// user doesn't exist
	getUserInput := &iam.GetUserInput{
		UserName: aws.String(userName),
	}
	err = awserr.New(iam.ErrCodeNoSuchEntityException, "user not found", fmt.Errorf("user not found"))
	mockAwsClient.EXPECT().GetUser(getUserInput).Return(nil, err)

	// always a PutUserPolicy (even if user already exists)
	putUserPolicyInput := &iam.PutUserPolicyInput{
		UserName:       aws.String(userName),
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(policyDocument),
	}
	putUserPolicyOutput := &iam.PutUserPolicyOutput{}
	mockAwsClient.EXPECT().PutUserPolicy(putUserPolicyInput).Return(putUserPolicyOutput, nil)

	// expect a CreateUser
	createUserInput := &iam.CreateUserInput{
		UserName: aws.String(userName),
	}
	createUserOutput := &iam.CreateUserOutput{}
	mockAwsClient.EXPECT().CreateUser(createUserInput).Return(createUserOutput, nil)

	// expect a CreateAccessKey for the new user
	createAccessKeyInput := &iam.CreateAccessKeyInput{
		UserName: aws.String(userName),
	}
	createAccessKeyOutput := &iam.CreateAccessKeyOutput{
		AccessKey: &iam.AccessKey{
			AccessKeyId:     aws.String(testAccessKeyID),
			SecretAccessKey: aws.String(testSecretAccessKey),
		},
	}
	mockAwsClient.EXPECT().CreateAccessKey(createAccessKeyInput).Return(createAccessKeyOutput, nil)

}

func validateUnexpectedActions(t *testing.T, tLog log.FieldLogger, actions []clientgotesting.Action, unexpectedActions []expectedAction) {
	for _, uea := range unexpectedActions {
		for _, a := range actions {
			ns := a.GetNamespace()
			verb := a.GetVerb()
			res := a.GetResource()
			if ns == uea.namespace &&
				res == uea.gvr &&
				verb == uea.verb {
				t.Errorf("found unexpected kube action: %v", a)
			}
		}
	}
}

func validateExpectedActions(t *testing.T, tLog log.FieldLogger, actions []clientgotesting.Action, expectedActions []expectedAction) {
	anyMissing := false
	for _, ea := range expectedActions {
		found := false
		for _, a := range actions {
			ns := a.GetNamespace()
			res := a.GetResource()
			verb := a.GetVerb()
			if ns == ea.namespace &&
				res == ea.gvr &&
				verb == ea.verb {
				found = true
				if ea.validate != nil {
					ea.validate(t, a)
				}
			}
		}
		if !found {
			anyMissing = true
			assert.True(t, found, "unable to find expected action: %v", ea)
		}
	}
	if anyMissing {
		tLog.Warnf("actions found: %v", actions)
	}
}
func TestRegistryUserName(t *testing.T) {
	cases := []struct {
		name           string
		clusterID      string
		expectedResult string
	}{
		{
			name:           "test registry user",
			clusterID:      "testcluster-a123f",
			expectedResult: "testcluster-a123f-registry",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := getRegistryUserName(tc.clusterID)

			// Assert
			assert.True(t, result == tc.expectedResult, "expected %v got %v", tc.expectedResult, result)
		})
	}
}

func getKey(cluster *clusterapiv1.Cluster, t *testing.T) string {
	key, err := controller.KeyFunc(cluster)
	if err != nil {
		t.Errorf("Unexpected error getting key for cluster %v: %v", cluster.Name, err)
		return ""
	}
	return key
}
