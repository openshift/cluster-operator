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

package route53hostedzone

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/golang/mock/gomock"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws"
	"k8s.io/client-go/kubernetes"
)

type testContext struct {
	controller *Controller
	mocks      *mocks
}

// Sets up all of the mocks, informers, cache, etc for tests.
func setupTestContext(t *testing.T) *testContext {
	mocks := setupDefaultMocks(t)

	ctx := &testContext{
		mocks: mocks,
		controller: NewController(
			mocks.fakeDNSZonesInformer,
			mocks.fakeKubeClient,
			mocks.fakeClusteropClient,
		),
	}

	return ctx
}

// TestSyncHandler tests that the controller's syncHandler function correctly syncs the desired state with the current state.
func TestSyncHandler(t *testing.T) {
	cases := []struct {
		name                     string
		key                      string
		expectedErrString        string
		dnsZone                  *cov1.DNSZone
		expectHostedZoneCreation bool
		listHostedZonesOutput    *route53.ListHostedZonesOutput
		awsClientBuilderError    error
		listHostedZonesErr       error
	}{
		{
			name: "DNSZone not found, no errors",
			key:  "namespace/somezone",
		},
		{
			name: "DNSZone found, Create hostedzone as no corresponding route53 hostedzone exists",
			key:  validDNSZone.Namespace + "/" + validDNSZone.Name,
			expectHostedZoneCreation: true,
			dnsZone:                  validDNSZone.DeepCopy(),
			listHostedZonesOutput: &route53.ListHostedZonesOutput{
				HostedZones: []*route53.HostedZone{
					{
						Name: aws.String("not.the.right.zone."),
					},
				},
			},
		},
		{
			name: "DNSZone found, Status Generation doesn't match object generation (sync needed)",
			key:  validDNSZone.Namespace + "/" + validDNSZone.Name,
			expectHostedZoneCreation: true,
			dnsZone:                  validDNSZoneGensDontMatch.DeepCopy(),
			listHostedZonesOutput:    &route53.ListHostedZonesOutput{},
		},
		{
			name:                  "DNSZone found, In deleting state (sync needed)",
			key:                   validDNSZone.Namespace + "/" + validDNSZone.Name,
			dnsZone:               validDNSZoneBeingDeleted.DeepCopy(),
			listHostedZonesOutput: &route53.ListHostedZonesOutput{},
		},
		{
			name:    "DNSZone found, Status Generation Timestamp within sync period (no sync needed)",
			dnsZone: validDNSZoneGenTimestampSyncNotNeeded.DeepCopy(),
			key:     validDNSZone.Namespace + "/" + validDNSZone.Name,
		},
		{
			name:    "DNSZone found, Status Generation Timestamp outside sync period (sync needed)",
			dnsZone: validDNSZoneGenTimestampSyncNeeded.DeepCopy(),
			key:     validDNSZone.Namespace + "/" + validDNSZone.Name,
			expectHostedZoneCreation: true,
			listHostedZonesOutput:    &route53.ListHostedZonesOutput{},
		},
		{
			name:              "Splitting key errors",
			key:               "this/isnt/valid/namespace/key/value",
			expectedErrString: "unexpected key format: \"this/isnt/valid/namespace/key/value\"",
		},
		{
			name:                  "Error creating Cluster Operator AWS Client",
			key:                   validDNSZone.Namespace + "/" + validDNSZone.Name,
			dnsZone:               validDNSZone.DeepCopy(),
			awsClientBuilderError: errors.NewBadRequest("shame on you"),
			expectedErrString:     "shame on you",
		},
		{
			name:                  "Error reconciling desired state",
			key:                   validDNSZone.Namespace + "/" + validDNSZone.Name,
			dnsZone:               validDNSZone.DeepCopy(),
			listHostedZonesOutput: &route53.ListHostedZonesOutput{},
			listHostedZonesErr:    errors.NewBadRequest("shame on you"),
			expectedErrString:     "shame on you",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := setupTestContext(t)

			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			defer ctx.mocks.mockCtrl.Finish()

			if tc.dnsZone != nil {
				setFakeDNSZoneInKube(ctx.mocks, tc.dnsZone)
				ctx.controller.awsClientBuilder = func(kubeClient kubernetes.Interface, secretName, namespace, region string) (coaws.Client, error) {
					return ctx.mocks.mockAWSClient, tc.awsClientBuilderError
				}

				if tc.listHostedZonesOutput != nil {
					listHostedZonesMock := ctx.mocks.mockAWSClient.EXPECT().
						ListHostedZones(gomock.Any()).
						Return(tc.listHostedZonesOutput, tc.listHostedZonesErr).
						Times(1)

					if tc.expectHostedZoneCreation {
						ctx.mocks.mockAWSClient.EXPECT().
							CreateHostedZone(gomock.Any()).
							Return(nil, nil).
							Times(1).
							After(listHostedZonesMock)
					}
				}
			}

			// Act
			err := ctx.controller.syncHandler(tc.key)

			// Assert
			assertErrorNilOrMessage(t, err, tc.expectedErrString)
		})
	}
}
