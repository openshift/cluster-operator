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

package playbookmock

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	logger "github.com/sirupsen/logrus"
)

type action struct {
	Time    time.Time
	Payload map[string]interface{}
}

type server struct {
	rwMutex sync.RWMutex
	actions []action
}

// CreateHandler creates playbook_mock HTTP handler.
func createHandler() http.Handler {
	s := server{}

	var router = mux.NewRouter()

	router.HandleFunc("/", s.postPlaybookAction).Methods("POST")
	router.HandleFunc("/", s.getPlaybookActions).Methods("GET")

	return router
}

// Run creates the HTTP handler, and begins to listen on the specified address.
func Run(ctx context.Context, addr string) error {
	logger.Infof("Starting server on %s\n", addr)
	srv := &http.Server{
		Addr:    addr,
		Handler: createHandler(),
	}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if srv.Shutdown(c) != nil {
			srv.Close()
		}
	}()
	return srv.ListenAndServe()
}

func (s *server) postPlaybookAction(w http.ResponseWriter, r *http.Request) {
	logger.Infof("Post playbook action...")
	var payload map[string]interface{}
	if err := bodyToObject(r, &payload); err != nil {
		logger.Errorf("error unmarshalling: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	s.rwMutex.Lock()
	defer s.rwMutex.Unlock()
	s.actions = append(s.actions, action{Time: time.Now(), Payload: payload})
	w.WriteHeader(http.StatusOK)
}

func (s *server) getPlaybookActions(w http.ResponseWriter, r *http.Request) {
	logger.Infof("Get playbook actions...")
	s.rwMutex.Lock()
	defer s.rwMutex.Unlock()
	logger.Infof("actions = %+v", s.actions)
	data, err := json.Marshal(s.actions)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// bodyToObject will convert the incoming HTTP request into the
// passed in 'object'
func bodyToObject(r *http.Request, object interface{}) error {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(body, object)
	if err != nil {
		return err
	}

	return nil
}
