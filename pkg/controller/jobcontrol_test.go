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

package controller

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"

	log "github.com/sirupsen/logrus"
	testlog "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"

	kbatch "k8s.io/api/batch/v1"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	clusteroperator "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"
)

const (
	testJobPrefix = "test-job-"
	testOwnerKey  = "test-owner-key"
)

var (
	testOwnerKind = schema.FromAPIVersionAndKind("test-api", "test-kind")
)

type testJobOwnerControl struct {
	getOwnerKey func(owner metav1.Object) (string, error)

	getOwner func(namespace string, name string) (metav1.Object, error)

	onOwnedJobEvent func(owner metav1.Object)
}

func (c *testJobOwnerControl) GetOwnerKey(owner metav1.Object) (string, error) {
	if c.getOwnerKey != nil {
		return c.getOwnerKey(owner)
	}
	return KeyFunc(owner)
}

func (c *testJobOwnerControl) GetOwner(namespace string, name string) (metav1.Object, error) {
	if c.getOwner != nil {
		return c.getOwner(namespace, name)
	}
	return nil, fmt.Errorf("not found")
}

func (c *testJobOwnerControl) OnOwnedJobEvent(owner metav1.Object) {
	if c.onOwnedJobEvent != nil {
		c.onOwnedJobEvent(owner)
		return
	}
	// Do nothing
}

type testJobFactory struct {
	responseJob       *kbatch.Job
	responseConfigMap *kapi.ConfigMap
	responseError     error
	calls             []string
}

func newTestJobFactory(job *kbatch.Job, configMap *kapi.ConfigMap, err error) *testJobFactory {
	factory := &testJobFactory{
		responseError: err,
	}
	if job != nil {
		factory.responseJob = job.DeepCopy()
	}
	if configMap != nil {
		factory.responseConfigMap = configMap.DeepCopy()
	}
	return factory
}

func (f *testJobFactory) BuildJob(name string) (*kbatch.Job, *kapi.ConfigMap, error) {
	f.calls = append(f.calls, name)
	return f.responseJob, f.responseConfigMap, f.responseError
}

func newTestJobControl(jobPrefix string, ownerKind schema.GroupVersionKind) (
	*jobControl,
	cache.Store, // job store
	*clientgofake.Clientset,
	kubeinformers.SharedInformerFactory,
	*testJobOwnerControl,
	log.FieldLogger,
	*testlog.Hook,
) {
	kubeClient := &clientgofake.Clientset{}
	kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	// Ensure that the return from creating a job is the job created, since
	// jobControl.createJob uses the returned job.
	kubeClient.AddReactor("create", "jobs", func(action clientgotesting.Action) (bool, kruntime.Object, error) {
		return true, action.(clientgotesting.CreateAction).GetObject(), nil
	})

	jobOwnerControl := &testJobOwnerControl{}

	logger, hook := getTestLogger()

	c := NewJobControl(
		jobPrefix,
		ownerKind,
		kubeClient,
		kubeInformers.Batch().V1().Jobs().Lister(),
		jobOwnerControl,
		logger,
	)

	return c.(*jobControl),
		kubeInformers.Batch().V1().Jobs().Informer().GetStore(),
		kubeClient,
		kubeInformers,
		jobOwnerControl,
		logger,
		hook
}

func newTestOwner(generation int64) metav1.Object {
	return &metav1.ObjectMeta{
		Name:       "test-owner",
		Generation: generation,
	}
}

func newTestJob(name string) *kbatch.Job {
	return &kbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newTestControlledJob(namePrefix, nameEnding string, owner metav1.Object, ownerKind schema.GroupVersionKind, generation int64) *kbatch.Job {
	return &kbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            namePrefix + nameEnding,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(owner, ownerKind)},
			Annotations:     map[string]string{clusteroperator.OwnerGenerationAnnotation: strconv.FormatInt(generation, 10)},
		},
	}
}

func newTestConfigMap(name string) *kapi.ConfigMap {
	return &kapi.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// TestJobControlWithoutNeedForNewJob tests controlling jobs when a new job
// is not needed.
func TestJobControlWithoutNeedForNewJob(t *testing.T) {
	jobControl, _, _, _, _, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	job, isNew, err := jobControl.ControlJobs(testOwnerKey, testOwner, false, nil)
	if err != nil {
		t.Fatalf("no error expected: %v", err)
	}
	if job != nil {
		t.Fatalf("no job expected: %v", job)
	}
	if isNew {
		t.Fatalf("job should not be created")
	}
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWithPendingExpectations tests controlling jobs when there
// are pending expectations that have not yet been met.
func TestJobControlWithPendingExpectations(t *testing.T) {
	jobControl, _, _, _, _, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobControl.expectations.ExpectCreations(testOwnerKey, 1)
	jobFactory := newTestJobFactory(nil, nil, nil)
	job, isNew, err := jobControl.ControlJobs(testOwnerKey, testOwner, true, jobFactory)
	if err != nil {
		t.Fatalf("no error expected: %v", err)
	}
	if job != nil {
		t.Fatalf("no job expected: %v", job)
	}
	if isNew {
		t.Fatalf("job should not be created")
	}
	if len(jobFactory.calls) > 0 {
		t.Fatalf("should not build new jobs while expectations pending")
	}
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlForNewJob tests controlling jobs when there are no existing
// jobs and a new job is needed.
func TestJobControlForNewJob(t *testing.T) {
	jobControl, _, kubeClient, _, _, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	newJob := newTestJob("new-job")
	newConfigMap := newTestConfigMap("new-configmap")
	jobFactory := newTestJobFactory(newJob, newConfigMap, nil)
	job, isNew, err := jobControl.ControlJobs(testOwnerKey, testOwner, true, jobFactory)
	if err != nil {
		t.Fatalf("no error expected: %v", err)
	}
	if job == nil {
		t.Fatalf("job expected")
	}
	if !isNew {
		t.Fatalf("job should be created")
	}
	if e, a := 1, len(jobFactory.calls); e != a {
		t.Fatalf("unexpected number of calls to build jobs: expected %v, got %v", e, a)
	}
	if e, a := newJob.Name, job.Name; e != a {
		t.Fatalf("unexpected job created: expected %v, got %v", e, a)
	}
	actions := kubeClient.Actions()
	if e, a := 2, len(actions); e != a {
		t.Fatalf("unexpected number of kube client actions: expected %v, got %v", e, a)
	}
	{
		createAction, ok := actions[0].(clientgotesting.CreateAction)
		if !ok {
			t.Fatalf("first action was not a create: %v", actions[0])
		}
		createdObject := createAction.GetObject()
		configMap, ok := createdObject.(*kapi.ConfigMap)
		if !ok {
			t.Fatalf("first action created object is not a configmap")
		}
		if e, a := newConfigMap.Name, configMap.Name; e != a {
			t.Fatalf("created configmap does not match expected: expected %v, got %v", e, a)
		}
	}
	{
		createAction, ok := actions[1].(clientgotesting.CreateAction)
		if !ok {
			t.Fatalf("second action was not a create: %v", actions[0])
		}
		createdObject := createAction.GetObject()
		createdJob, ok := createdObject.(*kbatch.Job)
		if !ok {
			t.Fatalf("second action created object is not a job")
		}
		if e, a := newJob.Name, createdJob.Name; e != a {
			t.Fatalf("created job does not match expected: expected %v, got %v", e, a)
		}
	}
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlForExistingJob tests controlling jobs when there is an
// existing job for the current generation of the owner.
func TestJobControlForExistingJob(t *testing.T) {
	jobControl, jobStore, kubeClient, _, _, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	existingJob := newTestControlledJob(testJobPrefix, "existing-job", testOwner, testOwnerKind, 1)
	jobStore.Add(existingJob)
	newJob := newTestJob("new-job")
	newConfigMap := newTestConfigMap("new-configmap")
	jobFactory := newTestJobFactory(newJob, newConfigMap, nil)
	job, isNew, err := jobControl.ControlJobs(testOwnerKey, testOwner, true, jobFactory)
	if err != nil {
		t.Fatalf("no error expected: %v", err)
	}
	if job == nil {
		t.Fatalf("job expected")
	}
	if isNew {
		t.Fatalf("job should not be created")
	}
	if e, a := 0, len(jobFactory.calls); e != a {
		t.Fatalf("unexpected number of calls to build jobs: expected %v, got %v", e, a)
	}
	if e, a := existingJob.Name, job.Name; e != a {
		t.Fatalf("unexpected job returned: expected %v, got %v", e, a)
	}
	actions := kubeClient.Actions()
	if e, a := 0, len(actions); e != a {
		t.Fatalf("unexpected number of kube client actions: expected %v, got %v", e, a)
	}
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlForExistingOldJob tests controlling jobs when there is an
// existing job for an older generation of the owner.
func TestJobControlForExistingOldJob(t *testing.T) {
	jobControl, jobStore, kubeClient, _, _, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(2)
	existingJob := newTestControlledJob(testJobPrefix, "existing-job", testOwner, testOwnerKind, 1)
	jobStore.Add(existingJob)
	newJob := newTestJob("new-job")
	newConfigMap := newTestConfigMap("new-configmap")
	jobFactory := newTestJobFactory(newJob, newConfigMap, nil)
	job, _, err := jobControl.ControlJobs(testOwnerKey, testOwner, true, jobFactory)
	if err != nil {
		t.Fatalf("no error expected: %v", err)
	}
	if job != nil {
		t.Fatalf("unexpected job: %v", job)
	}
	if e, a := 0, len(jobFactory.calls); e != a {
		t.Fatalf("unexpected number of calls to build jobs: expected %v, got %v", e, a)
	}
	actions := kubeClient.Actions()
	if e, a := 1, len(actions); e != a {
		t.Fatalf("unexpected number of kube client actions: expected %v, got %v", e, a)
	}
	{
		deleteAction, ok := actions[0].(clientgotesting.DeleteAction)
		if !ok {
			t.Fatalf("first action was not a delete: %v", actions[0])
		}
		deletedObjectName := deleteAction.GetName()
		if e, a := existingJob.Name, deletedObjectName; e != a {
			t.Fatalf("deleted job does not match expected: expected %v, got %v", e, a)
		}
	}
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenJobDeleteFails tests controlling jobs when the delete
// of an old existing job fails.
func TestJobControlWhenJobDeleteFails(t *testing.T) {
	jobControl, jobStore, kubeClient, _, _, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	kubeClient.AddReactor("delete", "jobs", func(action clientgotesting.Action) (bool, kruntime.Object, error) {
		return true, nil, fmt.Errorf("delete failed")
	})
	testOwner := newTestOwner(2)
	existingJob := newTestControlledJob(testJobPrefix, "existing-job", testOwner, testOwnerKind, 1)
	jobStore.Add(existingJob)
	newJob := newTestJob("new-job")
	newConfigMap := newTestConfigMap("new-configmap")
	jobFactory := newTestJobFactory(newJob, newConfigMap, nil)
	_, _, err := jobControl.ControlJobs(testOwnerKey, testOwner, true, jobFactory)
	if err == nil {
		t.Fatalf("error expected")
	}
	if e, a := "delete failed", err.Error(); e != a {
		t.Fatalf("unexpected error: expected %v, got %v", e, a)
	}
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenControlledJobAdded tests adding a job that is controlled
// by the job control.
func TestJobControlWhenControlledJobAdded(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return testOwner, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestControlledJob(testJobPrefix, "new-job", testOwner, testOwnerKind, 1)
	jobControl.OnAdd(newJob)
	assert.True(t, ownerEnqueued, "owner not enqueued")
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenUncontrolledJobAdded tests adding a job that is not
// controlled by the job control.
func TestJobControlWhenUncontrolledJobAdded(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return testOwner, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestJob("new-job")
	jobControl.OnAdd(newJob)
	assert.False(t, ownerEnqueued, "owner enqueued unexpectedly")
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenControlledJobAddedWithMissingOwner tests adding a job
// that is controlled by the job control but whose owner is missing.
func TestJobControlWhenControlledJobAddedWithMissingOwner(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return nil, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestControlledJob(testJobPrefix, "new-job", testOwner, testOwnerKind, 1)
	jobControl.OnAdd(newJob)
	assert.False(t, ownerEnqueued, "owner enqueued unexpectedly")
	assert.NotEmpty(t, getDireLogEntries(loggerHook), "expected dire log entries")
}

// TestJobControlWhenControlledJobUpdated tests updating a job that is
// controlled by the job control.
func TestJobControlWhenControlledJobUpdated(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return testOwner, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestControlledJob(testJobPrefix, "new-job", testOwner, testOwnerKind, 1)
	jobControl.OnUpdate(newJob, newJob)
	assert.True(t, ownerEnqueued, "owner not enqueued")
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenUncontrolledJobUpdated tests updating a job that is not
// controlled by the job control.
func TestJobControlWhenUncontrolledJobUpdated(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return testOwner, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestJob("new-job")
	jobControl.OnUpdate(newJob, newJob)
	assert.False(t, ownerEnqueued, "owner enqueued unexpectedly")
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenControlledJobUpdatedWithMissingOwner tests updating a job
// that is controlled by the job control but whose owner is missing.
func TestJobControlWhenControlledJobUpdatedWithMissingOwner(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return nil, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestControlledJob(testJobPrefix, "new-job", testOwner, testOwnerKind, 1)
	jobControl.OnUpdate(newJob, newJob)
	assert.False(t, ownerEnqueued, "owner enqueued unexpectedly")
	assert.NotEmpty(t, getDireLogEntries(loggerHook), "expected dire log entries")
}

// TestJobControlWhenControlledJobDeleted tests deleting a job that is
// controlled by the job control.
func TestJobControlWhenControlledJobDeleted(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return testOwner, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestControlledJob(testJobPrefix, "new-job", testOwner, testOwnerKind, 1)
	jobControl.OnDelete(newJob)
	assert.True(t, ownerEnqueued, "owner not enqueued")
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenUncontrolledJobDeleted tests deleting a job that is not
// controlled by the job control.
func TestJobControlWhenUncontrolledJobDeleted(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return testOwner, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestJob("new-job")
	jobControl.OnDelete(newJob)
	assert.False(t, ownerEnqueued, "owner enqueued unexpectedly")
	assert.Empty(t, getDireLogEntries(loggerHook), "unexpected dire log entries")
}

// TestJobControlWhenControlledJobDeletedWithMissingOwner tests deleting a job
// that is controlled by the job control but whose owner is missing.
func TestJobControlWhenControlledJobDeletedWithMissingOwner(t *testing.T) {
	jobControl, _, _, _, jobOwnerControl, _, loggerHook := newTestJobControl(testJobPrefix, testOwnerKind)
	testOwner := newTestOwner(1)
	jobOwnerControl.getOwner = func(namespace string, name string) (metav1.Object, error) {
		return nil, nil
	}
	ownerEnqueued := false
	jobOwnerControl.onOwnedJobEvent = func(owner metav1.Object) {
		assert.Equal(t, testOwner, owner, "unexpected enqueued owner")
		ownerEnqueued = true
	}
	newJob := newTestControlledJob(testJobPrefix, "new-job", testOwner, testOwnerKind, 1)
	jobControl.OnDelete(newJob)
	assert.False(t, ownerEnqueued, "owner enqueued unexpectedly")
	assert.NotEmpty(t, getDireLogEntries(loggerHook), "expected dire log entries")
}

func getTestLogger() (log.FieldLogger, *testlog.Hook) {
	logger := log.StandardLogger()
	hook := testlog.NewLocal(logger)
	function, _, _, _ := runtime.Caller(1)
	fieldLogger := logger.WithField("test", runtime.FuncForPC(function).Name())
	return fieldLogger, hook
}

func getDireLogEntries(hook *testlog.Hook) []*log.Entry {
	direEntries := []*log.Entry{}
	for _, entry := range hook.Entries {
		if entry.Level < log.InfoLevel {
			direEntries = append(direEntries, entry)
		}
	}
	return direEntries
}
