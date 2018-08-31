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

package registryinfra

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	coansible "github.com/openshift/cluster-operator/pkg/ansible"
	cometrics "github.com/openshift/cluster-operator/pkg/kubernetes/pkg/util/metrics"
	cologging "github.com/openshift/cluster-operator/pkg/logging"

	cocontroller "github.com/openshift/cluster-operator/pkg/controller"

	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
	coclientset "github.com/openshift/cluster-operator/pkg/client/clientset_generated/clientset"
	coinformers "github.com/openshift/cluster-operator/pkg/client/informers_generated/externalversions/clusteroperator/v1alpha1"
	colisters "github.com/openshift/cluster-operator/pkg/client/listers_generated/clusteroperator/v1alpha1"

	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	clusterapiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"
	capilister "sigs.k8s.io/cluster-api/pkg/client/listers_generated/cluster/v1alpha1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	coaws "github.com/openshift/cluster-operator/pkg/clusterapi/aws"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	controllerName = "registryinfra"

	infraDeployed          = "RegistryInfraDeployed"
	infraDeploymentFailure = "RegistryInfraFailure"

	// watch out for leading newline in policy JSON
	// AWS will reject the policy with unhelpful error messages
	// This policy taken from
	// https://docs.docker.com/registry/storage-drivers/s3/#s3-permission-scopes
	s3PolicyTemplate = `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:GetBucketLocation",
                "s3:ListBucket",
                "s3:ListBucketMultipartUploads"
            ],
            "Resource": "arn:aws:s3:::{{ .BucketName }}"
        },
        {
            "Effect": "Allow",
            "Action": [
                "s3:AbortMultipartUpload",
                "s3:DeleteObject",
                "s3:GetObject",
                "s3:ListMultipartUploadParts",
                "s3:PutObject"
            ],
            "Resource": "arn:aws:s3:::{{ .BucketName }}/*"
        }
    ]
}`

	// allow opting out of deploying with an S3-backed registry (must be set to "false" otherwise
	// assumed to be "true")
	s3RegistryOptOutAnnotation = "clusteroperator.openshift.io/s3-backed-registry"
)

type s3PolicyTemplateParams struct {
	BucketName string
}

// NewController returns a new *Controller to use with
// cluster-api resources.
func NewController(
	clusterInformer clusterapiinformers.ClusterInformer,
	clusterDeploymentInformer coinformers.ClusterDeploymentInformer,
	kubeClient kubeclientset.Interface,
	clusteroperatorClient coclientset.Interface,
	clusterapiClient clusterapiclientset.Interface,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient != nil && kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		cometrics.RegisterMetricAndTrackRateLimiterUsage(
			fmt.Sprintf("clusteroperator_%s_controller", controllerName),
			kubeClient.CoreV1().RESTClient().GetRateLimiter(),
		)
	}

	logger := log.WithField("controller", controllerName)
	c := &Controller{
		kubeClient:              kubeClient,
		coClient:                clusteroperatorClient,
		capiClient:              clusterapiClient,
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName),
		logger:                  logger,
		clusterLister:           clusterInformer.Lister(),
		clusterDeploymentLister: clusterDeploymentInformer.Lister(),
		clustersSynced:          clusterInformer.Informer().HasSynced,
		awsClientBuilder:        coaws.NewClient,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
		DeleteFunc: c.deleteCluster,
	})

	c.syncHandler = c.syncCluster
	c.enqueueCluster = c.enqueue

	c.awsEmptyBucket = c.emptyS3Bucket

	return c
}

// Controller manages clusters.
type Controller struct {
	kubeClient kubeclientset.Interface
	coClient   coclientset.Interface
	capiClient clusterapiclientset.Interface

	// To allow injection of syncCluster for testing.
	syncHandler func(hKey string) error

	// To allow injection of mock ansible generator for testing
	ansibleGenerator coansible.JobGenerator

	// used for unit testing
	enqueueCluster func(cluster metav1.Object)

	clusterLister           capilister.ClusterLister
	clusterDeploymentLister colisters.ClusterDeploymentLister

	// clustersSynced returns true if the cluster shared informer has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	clustersSynced cache.InformerSynced

	// Clusters that need to be synced
	queue workqueue.RateLimitingInterface

	logger *log.Entry

	// AWS client builder
	awsClientBuilder func(kubeClient kubeclientset.Interface, secretName, namespace, region string) (coaws.Client, error)

	// AWS bucket emptier
	awsEmptyBucket func(bucketName string, awsClient coaws.Client) error
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*capiv1.Cluster)
	cologging.WithCluster(c.logger, cluster).Debugf("Adding cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) updateCluster(old, obj interface{}) {
	cluster := obj.(*capiv1.Cluster)
	cologging.WithCluster(c.logger, cluster).Debugf("Updating cluster")
	c.enqueueCluster(cluster)
}

func (c *Controller) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*capiv1.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*capiv1.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not an Object %#v", obj))
			return
		}
	}
	cologging.WithCluster(c.logger, cluster).Debugf("Deleting cluster")
	c.enqueueCluster(cluster)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("starting %s controller", controllerName)
	defer c.logger.Infof("shutting down %s controller", controllerName)

	if !cocontroller.WaitForCacheSync("registryinfra", stopCh, c.clustersSynced) {
		c.logger.Errorf("Could not sync caches for %s controller", controllerName)
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(cluster metav1.Object) {
	key, err := cocontroller.KeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", cluster, err))
		return
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	logger := c.logger.WithField("cluster", key)

	logger.Errorf("error syncing registry infra: %v", err)
	if c.queue.NumRequeues(key) < maxRetries {
		logger.Errorf("retrying registry infra")
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("dropping cluster out of the queue: %v", err)
	c.queue.Forget(key)
}

func (c *Controller) syncCluster(key string) error {
	startTime := time.Now()
	c.logger.WithField("key", key).Debug("syncing registry infra")
	defer func() {
		c.logger.WithFields(log.Fields{
			"key":      key,
			"duration": time.Now().Sub(startTime),
		}).Debug("finished syncing registry infra")
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	capiCluster, err := c.clusterLister.Clusters(namespace).Get(name)
	if errors.IsNotFound(err) {
		c.logger.WithField("key", key).Debug("cluster has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	if val, ok := capiCluster.Annotations[s3RegistryOptOutAnnotation]; ok && val == "false" {
		// skip processing this cluster as it doesn't want an s3-backed registry
		c.logger.WithField("cluster", capiCluster.Name).Debug("cluster doesn't want s3-backed registry. skipping.")

		// but update cluster object to indicate that the controller has processed the cluster object
		err = c.updateClusterStatus(capiCluster, true, nil)
		if err != nil {
			return err
		}
		return nil
	}

	cluster, err := cocontroller.ConvertToCombinedCluster(capiCluster)
	if err != nil {
		return fmt.Errorf("failure converting to combined cluster: %v", err)
	}

	clusterProviderStatus, err := cocontroller.ClusterProviderStatusFromCluster(capiCluster)
	if err != nil {
		return fmt.Errorf("error retrieving clusterproviderstatus: %v", err)
	}

	// check if the cluster is being deleted
	if capiCluster.DeletionTimestamp != nil {
		c.logger.WithField("cluster", key).Debug("cleaning up registry infrastructure pieces")

		err = c.syncRegistryInfra(cluster, false /* deprovision infra */)
		if err != nil {
			return fmt.Errorf("error deleting registry infra: %v", err)
		}
		err = c.deleteFinalizer(capiCluster)
		return err
	}

	if clusterGenerationAlreadyProcessed(capiCluster, clusterProviderStatus) {
		// nothing to do as we've already handled this generation of the cluster
		return nil
	}

	if !hasRegistryInfraFinalizer(capiCluster) {
		c.logger.Debugf("adding registryinfra finalizer to cluster %v", capiCluster.Name)
		return c.addFinalizer(capiCluster)
	}

	// create infra if needed
	c.logger.Debugf("creating registry infrastructure pieces for cluster %v", capiCluster.Name)

	err = c.syncRegistryInfra(cluster, true /* create infra */)
	if err != nil {
		c.updateClusterStatus(capiCluster, false, err)
		return fmt.Errorf("error setting up infrastructure for registry: %v", err)
	}

	err = c.updateClusterStatus(capiCluster, true, nil)
	if err != nil {
		return fmt.Errorf("error updating cluster status: %v", err)
	}

	return nil
}

// configureAWS will return a client for interacting with AWS for the provided cluster
func (c *Controller) configureAWS(cluster *cov1.CombinedCluster) (coaws.Client, error) {
	adminCredsSecretName := cluster.AWSClusterProviderConfig.Hardware.AccountSecret.Name
	region := cluster.AWSClusterProviderConfig.Hardware.Region

	client, err := c.awsClientBuilder(c.kubeClient, adminCredsSecretName, cluster.Namespace, region)
	if err != nil {
		return nil, fmt.Errorf("error setting up aws client: %v", err)
	}
	return client, nil

}

func (c *Controller) emptyS3Bucket(bucketName string, awsClient coaws.Client) error {
	s3API := awsClient.GetS3API()
	iter := s3manager.NewDeleteListIterator(s3API, &s3.ListObjectsInput{
		Bucket: aws.String(bucketName),
	})
	err := s3manager.NewBatchDeleteWithClient(s3API).Delete(aws.BackgroundContext(), iter)
	if err != nil {
		return fmt.Errorf("error deleting objects from bucket %v: %v", bucketName, err)
	}

	return nil
}

func (c *Controller) deprovisionS3Bucket(bucketName string, awsClient coaws.Client) error {

	s3Bucket, err := getBucketByName(bucketName, awsClient)
	if err != nil {
		return fmt.Errorf("error searching for existing bucket: %v", err)
	}

	if s3Bucket == nil {
		return nil
	}

	// first we need to empty all the objects out of the bucket
	err = c.awsEmptyBucket(bucketName, awsClient)
	if err != nil {
		return err
	}

	// now we can delete the bucket
	_, err = awsClient.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("error deleting bucket: %v", err)
	}

	return nil
}

// create S3 bucket (if necessary) for cluster's registry
func (c *Controller) setupS3Bucket(bucketName string, awsClient coaws.Client) error {

	s3Bucket, err := getBucketByName(bucketName, awsClient)
	if err != nil {
		return fmt.Errorf("error searching for existing bucket: %v", err)
	}

	if s3Bucket != nil {
		// already have a bucket, don't try to re-create it
		return nil
	}

	if s3Bucket == nil {
		// bucket doesn't exist, need to create
		_, err := awsClient.CreateBucket(&s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			return fmt.Errorf("error creating bucket: %v", err)
		}
	}

	return nil
}

func deprovisionIamUserCreds(userName string, awsClient coaws.Client) error {
	// need to delete all access keys associated with the IAM user
	// to be able to eventually delete the user
	accessKeys, err := awsClient.ListAccessKeys(&iam.ListAccessKeysInput{
		UserName: aws.String(userName),
	})
	if err != nil {
		return fmt.Errorf("error listing access keys: %v", err)
	}
	for _, aKey := range accessKeys.AccessKeyMetadata {
		_, err = awsClient.DeleteAccessKey(&iam.DeleteAccessKeyInput{
			UserName:    aws.String(userName),
			AccessKeyId: aKey.AccessKeyId,
		})
		if err != nil {
			return fmt.Errorf("error deleting user's access key: %v", err)
		}
	}

	return nil
}

// setupIamUserCreds unconditionally creates and returns access/secret key for iam userName
func (c *Controller) setupIamUserCreds(userName string, awsClient coaws.Client) (*iam.AccessKey, error) {
	result, err := awsClient.CreateAccessKey(&iam.CreateAccessKeyInput{
		UserName: &userName,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating iam creds for user %v: %v", userName, err)
	}

	return result.AccessKey, nil
}

func deprovisionS3IamUser(clusterID, bucketName string, awsClient coaws.Client) error {
	existingUser, err := findExistingRegistryUser(clusterID, awsClient)
	if err != nil {
		return err
	}

	if existingUser != nil {
		// delete inline policy before deleting user
		err = deprovisionS3IamUserPolicy(*existingUser.UserName, awsClient)
		if err != nil {
			return err
		}

		// delete iam creds before deleting user
		err = deprovisionIamUserCreds(*existingUser.UserName, awsClient)
		if err != nil {
			return err
		}

		_, err := awsClient.DeleteUser(&iam.DeleteUserInput{
			UserName: existingUser.UserName,
		})
		if err != nil {
			return fmt.Errorf("error deleting IAM user: %v", err)
		}
		return nil
	}

	return nil
}

// setupS3IamUser creates an IAM user with an inline policy granting r/w permission on bucketName
// return iam creds if we are told that there are no exising credentials
func (c *Controller) setupS3IamUser(clusterID, bucketName string, awsClient coaws.Client) (*iam.AccessKey, error) {

	existingUser, err := findExistingRegistryUser(clusterID, awsClient)
	if err != nil {
		return nil, err
	}

	userName := getRegistryUserName(clusterID)

	if existingUser == nil {
		// create user
		_, err := awsClient.CreateUser(&iam.CreateUserInput{
			UserName: aws.String(userName),
		})
		if err != nil {
			return nil, fmt.Errorf("error creating iam user: %v", err)
		}
	}

	// attach policy to user
	err = c.s3IamUserPolicy(clusterID, userName, bucketName, awsClient)
	if err != nil {
		return nil, err
	}

	// create AWS access/secret for user if we haven't previously generated
	// credentials for this user
	var accessKey *iam.AccessKey
	if existingUser == nil {
		accessKey, err = c.setupIamUserCreds(userName, awsClient)
		if err != nil {
			return nil, err
		}
	}

	// return a nil accessKey if we didn't generate new creds (b/c they already exist)
	return accessKey, nil
}

func deprovisionS3IamUserPolicy(userName string, awsClient coaws.Client) error {
	// need to remove all inline policies to be able to remove user
	userPolicies, err := awsClient.ListUserPolicies(&iam.ListUserPoliciesInput{
		UserName: aws.String(userName),
	})
	if err != nil {
		return fmt.Errorf("error listing user inline policies: %v", err)
	}
	for _, policy := range userPolicies.PolicyNames {
		_, err = awsClient.DeleteUserPolicy(&iam.DeleteUserPolicyInput{
			UserName:   aws.String(userName),
			PolicyName: policy,
		})
		if err != nil {
			return fmt.Errorf("error deleting inline user policies: %v", err)
		}
	}

	return nil
}

// s3IamUserPolicy will attach iam policy to user granting r/w access to S3 bucketName
func (c *Controller) s3IamUserPolicy(clusterID, userName, bucketName string, awsClient coaws.Client) error {
	policyName := clusterID + "S3Access"
	policyDocument, err := createS3IamPolicy(bucketName)
	if err != nil {
		return err
	}

	_, err = awsClient.PutUserPolicy(&iam.PutUserPolicyInput{
		UserName:       aws.String(userName),
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(policyDocument),
	})
	if err != nil {
		return fmt.Errorf("error attaching policy to user: %v", err)
	}

	return nil
}

func createS3IamPolicy(bucketName string) (string, error) {
	t, err := template.New("s3policy").Parse(s3PolicyTemplate)
	if err != nil {
		return "", fmt.Errorf("error parsing s3policy template: %v", err)
	}

	params := s3PolicyTemplateParams{
		BucketName: bucketName,
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", fmt.Errorf("error processing s3policy template: %v", err)
	}
	policyDocument := buf.String()

	return policyDocument, nil
}

func (c *Controller) syncRegistryInfra(cluster *cov1.CombinedCluster, createInfra bool) error {
	clusterID := cluster.Name

	bucketName := cocontroller.RegistryObjectStoreName(clusterID)

	awsClient, err := c.configureAWS(cluster)
	if err != nil {
		return fmt.Errorf("error configuring AWS connection settings: %v", err)
	}

	if createInfra {
		// create bucket if necessary
		err = c.setupS3Bucket(bucketName, awsClient)
		if err != nil {
			return err
		}

		// create user if necessary
		newAccessKey, err := c.setupS3IamUser(clusterID, bucketName, awsClient)
		if err != nil {
			return err
		}

		// save iam user's AWS creds into secret
		err = c.setupRegistrySecret(clusterID, cluster.Namespace, newAccessKey)
		if err != nil {
			return err
		}

		// end of provisioning registry infra
		return nil
	}

	//else deprovision
	err = c.deprovisionS3Bucket(bucketName, awsClient)
	if err != nil {
		return fmt.Errorf("error deleting bucket: %v", err)
	}

	err = deprovisionS3IamUser(clusterID, bucketName, awsClient)
	if err != nil {
		return fmt.Errorf("error deleting registry IAM user: %v", err)
	}

	secretName := cocontroller.RegistryCredsSecretName(clusterID)
	err = c.deleteRegistyUserSecret(secretName, cluster.Namespace)
	if err != nil {
		return err
	}
	return nil
}

// deleteRegistryUserSecret will delete secret holding IAM registry user's cloud creds
func (c *Controller) deleteRegistyUserSecret(secretName string, secretNamespace string) error {
	_, err := c.kubeClient.CoreV1().Secrets(secretNamespace).Get(secretName, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error querying for existing registry secret: %v", err)
	} else if err == nil {
		// found existing secret
		err = c.kubeClient.CoreV1().Secrets(secretNamespace).Delete(secretName, &metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("error deleting registry secret: %v", err)
		}
	}
	return nil
}

// setupRegistrySecret will save IAM registry user's cloud creds into secret for later
// use when setting up the cluster registry
func (c *Controller) setupRegistrySecret(clusterID, secretNamespace string, accessKey *iam.AccessKey) error {

	if accessKey == nil {
		// accessKey == nil means no access key to store
		return nil
	}

	registrySecretAlreadyExists := false

	// name for secret where we will store the registry object store credentials
	objectStoreSecretName := cocontroller.RegistryCredsSecretName(clusterID)

	_, err := c.kubeClient.CoreV1().Secrets(secretNamespace).Get(objectStoreSecretName, metav1.GetOptions{})
	secretFound := (err == nil)
	unexpectedError := (err != nil && !errors.IsNotFound(err))

	if unexpectedError {
		return fmt.Errorf("error querying for existing registry secret: %v", err)
	} else if secretFound {
		registrySecretAlreadyExists = true
	}

	if registrySecretAlreadyExists {
		// delete the current secret before saving the new accessKey creds
		c.deleteRegistyUserSecret(objectStoreSecretName, secretNamespace)
	}

	// now create the secret with the creds
	_, err = c.kubeClient.CoreV1().Secrets(secretNamespace).Create(&kapi.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectStoreSecretName,
			Namespace: secretNamespace,
		},
		StringData: map[string]string{
			"awsAccessKeyId":     *accessKey.AccessKeyId,
			"awsSecretAccessKey": *accessKey.SecretAccessKey,
		},
	})
	if err != nil {
		return fmt.Errorf("error saving aws creds into registry secret: %v", err)
	}

	return nil
}

func (c *Controller) updateClusterStatus(cluster *capiv1.Cluster, success bool, syncError error) error {
	clusterProviderStatus, err := cocontroller.ClusterProviderStatusFromCluster(cluster)
	if err != nil {
		return fmt.Errorf("error retrieving cluster status: %v", err)
	}

	newConditions := c.updateClusterCondition(clusterProviderStatus, success, syncError)
	clusterProviderStatus.Conditions = newConditions

	clusterProviderStatus.RegistryInfraCompleted = success

	if success {
		clusterProviderStatus.RegistryInfraInstalledGeneration = cluster.Generation
	}

	raw, err := cocontroller.EncodeClusterProviderStatus(clusterProviderStatus)
	if err != nil {
		return fmt.Errorf("error converting clusterproviderstatus: %v", err)
	}

	clusterCopy := cluster.DeepCopy()
	clusterCopy.Status.ProviderStatus = raw

	err = cocontroller.UpdateClusterStatus(c.capiClient, clusterCopy)
	if err != nil {
		return fmt.Errorf("error updating cluster status: %v", err)
	}

	return nil
}

func (c *Controller) updateClusterCondition(clusterProviderStatus *cov1.ClusterProviderStatus, suceeded bool, syncError error) []cov1.ClusterCondition {
	type ConditionFields struct {
		condition   cov1.ClusterConditionType
		status      kapi.ConditionStatus
		msg         string
		reason      string
		updateCheck cocontroller.UpdateConditionCheck
	}
	conditions := []ConditionFields{}

	if suceeded {
		// set conditions indicating RegistryInfraProvisioned=true and RegistryInfraProvisioningFailed=false
		conditions = append(conditions, ConditionFields{
			condition:   cov1.RegistryInfraProvisioned,
			status:      kapi.ConditionTrue,
			msg:         fmt.Sprintf("registry infrastructure deployed/configured"),
			reason:      infraDeployed,
			updateCheck: cocontroller.UpdateConditionIfReasonOrMessageChange,
		})
		conditions = append(conditions, ConditionFields{
			condition:   cov1.RegistryInfraProvisioningFailed,
			status:      kapi.ConditionFalse,
			msg:         fmt.Sprintf("registry infra provisioning has not failed"),
			reason:      infraDeployed,
			updateCheck: cocontroller.UpdateConditionIfReasonOrMessageChange,
		})
	} else {
		// set conditions indicating RegistryInfraProvisioned=false and RegistryInfraProvisioningFailed=true
		conditions = append(conditions, ConditionFields{
			condition:   cov1.RegistryInfraProvisioningFailed,
			status:      kapi.ConditionTrue,
			msg:         fmt.Sprintf("registry infrastructure deployment failed: %v", syncError),
			reason:      infraDeploymentFailure,
			updateCheck: cocontroller.UpdateConditionIfReasonOrMessageChange,
		})
		conditions = append(conditions, ConditionFields{
			condition:   cov1.RegistryInfraProvisioned,
			status:      kapi.ConditionFalse,
			msg:         fmt.Sprintf("registy infrastructure failed to provision"),
			reason:      fmt.Sprintf("error provisioning registry infra: %v", syncError),
			updateCheck: cocontroller.UpdateConditionIfReasonOrMessageChange,
		})
	}

	conditionsToReturn := clusterProviderStatus.Conditions
	for _, condition := range conditions {
		conditionsToReturn = cocontroller.SetClusterCondition(conditionsToReturn,
			condition.condition,
			condition.status,
			condition.reason,
			condition.msg,
			condition.updateCheck)
	}

	return conditionsToReturn
}

func hasRegistryInfraFinalizer(cluster *capiv1.Cluster) bool {
	return cocontroller.HasFinalizer(cluster, cov1.FinalizerRegistryInfra)
}

func (c *Controller) deleteFinalizer(cluster *capiv1.Cluster) error {
	cluster = cluster.DeepCopy()
	cocontroller.DeleteFinalizer(cluster, cov1.FinalizerRegistryInfra)
	_, err := c.capiClient.ClusterV1alpha1().Clusters(cluster.Namespace).UpdateStatus(cluster)
	return err
}

func (c *Controller) addFinalizer(cluster *capiv1.Cluster) error {
	cluster = cluster.DeepCopy()
	cocontroller.AddFinalizer(cluster, cov1.FinalizerRegistryInfra)
	_, err := c.capiClient.ClusterV1alpha1().Clusters(cluster.Namespace).UpdateStatus(cluster)
	return err
}

func getBucketByName(bucketName string, awsClient coaws.Client) (*s3.Bucket, error) {
	bucketList, err := awsClient.ListBuckets(nil)
	if err != nil {
		return nil, fmt.Errorf("error listing buckets: %v", err)
	}

	var s3Bucket *s3.Bucket
	for _, bucket := range bucketList.Buckets {
		if *bucket.Name == bucketName {
			s3Bucket = bucket
		}
	}

	return s3Bucket, nil
}

func getRegistryUserName(clusterID string) string {
	return clusterID + "-registry"
}

func findExistingRegistryUser(clusterID string, awsClient coaws.Client) (*iam.User, error) {
	userName := getRegistryUserName(clusterID)
	userAlreadyExists := true

	userResults, err := awsClient.GetUser(&iam.GetUserInput{
		UserName: &userName,
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case iam.ErrCodeNoSuchEntityException:
				userAlreadyExists = false
			default:
				return nil, fmt.Errorf("error querying iam user: %v", err)
			}
		} else {
			return nil, fmt.Errorf("error querying iam user: %v", err)
		}
	}

	if userAlreadyExists {
		return userResults.User, nil
	}

	// no user found
	return nil, nil
}

func clusterGenerationAlreadyProcessed(cluster *capiv1.Cluster, providerStatus *cov1.ClusterProviderStatus) bool {
	currentGeneration := cluster.Generation

	if currentGeneration == providerStatus.RegistryInfraInstalledGeneration {
		return true
	}

	return false
}
