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

package controller

import (
	"reflect"
	"runtime/debug"
	"testing"

	"github.com/staebler/boatswain/pkg/apis/boatswain/v1alpha1"
	boatswaininformers "github.com/staebler/boatswain/pkg/client/informers_generated/externalversions"
	v1alpha1informers "github.com/staebler/boatswain/pkg/client/informers_generated/externalversions/boatswain/v1alpha1"

	boatswainclientset "github.com/staebler/boatswain/pkg/client/clientset_generated/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/api/meta"
	clientgofake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

// newTestHostController creates a new test host controller injected with
// fake clients and returns:
//
// - a fake kubernetes core api client
// - a fake boatswain api client
// - a test controller
// - the shared informers for the boatswain v1alpha1 api
//
// If there is an error, newTestHostController calls 'Fatal' on the injected
// testing.T.
func newTestHostController(t *testing.T) (
	*clientgofake.Clientset,
	*boatswainclientset.Clientset,
	*HostController,
	v1alpha1informers.Interface) {
	// create a fake kube client
	fakeKubeClient := &clientgofake.Clientset{}
	// create a fake boatswain client
	fakeBoatswainClient := &boatswainclientset.Clientset{}

	// create informers
	informerFactory := boatswaininformers.NewSharedInformerFactory(fakeBoatswainClient, 0)
	boatswainSharedInformers := informerFactory.Boatswain().V1alpha1()

	// create a test controller
	testController := NewHostController(
		boatswainSharedInformers.Hosts(),
		fakeKubeClient,
		fakeBoatswainClient,
	)

	return fakeKubeClient, fakeBoatswainClient, testController, boatswainSharedInformers
}

func assertNumEvents(t *testing.T, strings []string, number int) {
	if e, a := number, len(strings); e != a {
		fatalf(t, "Unexpected number of events: expected %v, got %v;\nevents: %+v", e, a, strings)
	}
}

// failfFunc is a type that defines the common signatures of T.Fatalf and
// T.Errorf.
type failfFunc func(t *testing.T, msg string, args ...interface{})

func fatalf(t *testing.T, msg string, args ...interface{}) {
	t.Log(string(debug.Stack()))
	t.Fatalf(msg, args...)
}

func errorf(t *testing.T, msg string, args ...interface{}) {
	t.Log(string(debug.Stack()))
	t.Errorf(msg, args...)
}

// assertion and expectation methods:
//
// - assertX will call t.Fatalf
// - expectX will call t.Errorf and return a boolean, allowing you to drive a 'continue'
//   in a table-type test

func assertNumberOfActions(t *testing.T, actions []clientgotesting.Action, number int) {
	testNumberOfActions(t, "" /* name */, fatalf, actions, number)
}

func expectNumberOfActions(t *testing.T, name string, actions []clientgotesting.Action, number int) bool {
	return testNumberOfActions(t, name, errorf, actions, number)
}

func testNumberOfActions(t *testing.T, name string, f failfFunc, actions []clientgotesting.Action, number int) bool {
	logContext := ""
	if len(name) > 0 {
		logContext = name + ": "
	}

	if e, a := number, len(actions); e != a {
		t.Logf("%+v\n", actions)
		f(t, "%vUnexpected number of actions: expected %v, got %v;\nactions: %+v", logContext, e, a, actions)
		return false
	}

	return true
}

func assertGet(t *testing.T, action clientgotesting.Action, obj interface{}) {
	assertActionFor(t, action, "get", "" /* subresource */, obj)
}

func assertList(t *testing.T, action clientgotesting.Action, obj interface{}, listRestrictions clientgotesting.ListRestrictions) {
	assertActionFor(t, action, "list", "" /* subresource */, obj)
	// Cast is ok since in the method above it's checked to be ListAction
	assertListRestrictions(t, listRestrictions, action.(clientgotesting.ListAction).GetListRestrictions())
}

func assertCreate(t *testing.T, action clientgotesting.Action, obj interface{}) runtime.Object {
	return assertActionFor(t, action, "create", "" /* subresource */, obj)
}

func assertUpdate(t *testing.T, action clientgotesting.Action, obj interface{}) runtime.Object {
	return assertActionFor(t, action, "update", "" /* subresource */, obj)
}

func assertUpdateStatus(t *testing.T, action clientgotesting.Action, obj interface{}) runtime.Object {
	return assertActionFor(t, action, "update", "status", obj)
}

func assertUpdateReference(t *testing.T, action clientgotesting.Action, obj interface{}) runtime.Object {
	return assertActionFor(t, action, "update", "reference", obj)
}

func expectCreate(t *testing.T, name string, action clientgotesting.Action, obj interface{}) (runtime.Object, bool) {
	return testActionFor(t, name, errorf, action, "create", "" /* subresource */, obj)
}

func expectUpdate(t *testing.T, name string, action clientgotesting.Action, obj interface{}) (runtime.Object, bool) {
	return testActionFor(t, name, errorf, action, "update", "" /* subresource */, obj)
}

func expectUpdateStatus(t *testing.T, name string, action clientgotesting.Action, obj interface{}) (runtime.Object, bool) {
	return testActionFor(t, name, errorf, action, "update", "status", obj)
}

func assertDelete(t *testing.T, action clientgotesting.Action, obj interface{}) {
	assertActionFor(t, action, "delete", "" /* subresource */, obj)
}

func assertActionFor(t *testing.T, action clientgotesting.Action, verb, subresource string, obj interface{}) runtime.Object {
	r, _ := testActionFor(t, "" /* name */, fatalf, action, verb, subresource, obj)
	return r
}

func testActionFor(t *testing.T, name string, f failfFunc, action clientgotesting.Action, verb, subresource string, obj interface{}) (runtime.Object, bool) {
	logContext := ""
	if len(name) > 0 {
		logContext = name + ": "
	}

	if e, a := verb, action.GetVerb(); e != a {
		f(t, "%vUnexpected verb: expected %v, got %v\n\tactual action %q", logContext, e, a, action)
		return nil, false
	}

	var resource string

	switch obj.(type) {
	case *v1alpha1.Host:
		resource = "hosts"
	}

	if e, a := resource, action.GetResource().Resource; e != a {
		f(t, "%vUnexpected resource; expected %v, got %v", logContext, e, a)
		return nil, false
	}

	if e, a := subresource, action.GetSubresource(); e != a {
		f(t, "%vUnexpected subresource; expected %v, got %v", logContext, e, a)
		return nil, false
	}

	rtObject, ok := obj.(runtime.Object)
	if !ok {
		f(t, "%vObject %+v was not a runtime.Object", logContext, obj)
		return nil, false
	}

	paramAccessor, err := meta.Accessor(rtObject)
	if err != nil {
		f(t, "%vError creating ObjectMetaAccessor for param object %+v: %v", logContext, rtObject, err)
		return nil, false
	}

	var (
		objectMeta   metav1.Object
		fakeRtObject runtime.Object
	)

	switch verb {
	case "get":
		getAction, ok := action.(clientgotesting.GetAction)
		if !ok {
			f(t, "%vUnexpected type; failed to convert action %+v to DeleteAction", logContext, action)
			return nil, false
		}

		if e, a := paramAccessor.GetName(), getAction.GetName(); e != a {
			f(t, "%vUnexpected name: expected %v, got %v", logContext, e, a)
			return nil, false
		}

		return nil, true
	case "list":
		_, ok := action.(clientgotesting.ListAction)
		if !ok {
			f(t, "%vUnexpected type; failed to convert action %+v to ListAction", logContext, action)
			return nil, false
		}
		return nil, true
	case "delete":
		deleteAction, ok := action.(clientgotesting.DeleteAction)
		if !ok {
			f(t, "%vUnexpected type; failed to convert action %+v to DeleteAction", logContext, action)
			return nil, false
		}

		if e, a := paramAccessor.GetName(), deleteAction.GetName(); e != a {
			f(t, "%vUnexpected name: expected %v, got %v", logContext, e, a)
			return nil, false
		}

		return nil, true
	case "create":
		createAction, ok := action.(clientgotesting.CreateAction)
		if !ok {
			f(t, "%vUnexpected type; failed to convert action %+v to CreateAction", logContext, action)
			return nil, false
		}

		fakeRtObject = createAction.GetObject()
		objectMeta, err = meta.Accessor(fakeRtObject)
		if err != nil {
			f(t, "%vError creating ObjectMetaAccessor for %+v", logContext, fakeRtObject)
			return nil, false
		}
	case "update":
		updateAction, ok := action.(clientgotesting.UpdateAction)
		if !ok {
			f(t, "%vUnexpected type; failed to convert action %+v to UpdateAction", logContext, action)
			return nil, false
		}

		fakeRtObject = updateAction.GetObject()
		objectMeta, err = meta.Accessor(fakeRtObject)
		if err != nil {
			f(t, "%vError creating ObjectMetaAccessor for %+v", logContext, fakeRtObject)
			return nil, false
		}
	}

	if e, a := paramAccessor.GetName(), objectMeta.GetName(); e != a {
		f(t, "%vUnexpected name: expected %q, got %q", logContext, e, a)
		return nil, false
	}

	fakeValue := reflect.ValueOf(fakeRtObject)
	paramValue := reflect.ValueOf(obj)

	if e, a := paramValue.Type(), fakeValue.Type(); e != a {
		f(t, "%vUnexpected type of object passed to fake client; expected %v, got %v", logContext, e, a)
		return nil, false
	}

	return fakeRtObject, true
}

// assertListRestrictions compares expected Fields / Labels on a list options.
func assertListRestrictions(t *testing.T, e, a clientgotesting.ListRestrictions) {
	if el, al := e.Labels.String(), a.Labels.String(); el != al {
		fatalf(t, "ListRestrictions.Labels don't match, expected %q got %q", el, al)
	}
	if ef, af := e.Fields.String(), a.Fields.String(); ef != af {
		fatalf(t, "ListRestrictions.Fields don't match, expected %q got %q", ef, af)
	}
}
