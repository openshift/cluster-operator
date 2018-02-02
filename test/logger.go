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

package test

import (
	"runtime"

	log "github.com/sirupsen/logrus"
	testlog "github.com/sirupsen/logrus/hooks/test"
)

// Logger returns a logger to use in tests.
func Logger() (log.FieldLogger, *testlog.Hook) {
	logger := log.StandardLogger()
	hook := testlog.NewLocal(logger)
	function, _, _, _ := runtime.Caller(1)
	fieldLogger := logger.WithField("test", runtime.FuncForPC(function).Name())
	return fieldLogger, hook
}

// GetDireLogEntries returns the log entries that are above Info level.
func GetDireLogEntries(hook *testlog.Hook) []*log.Entry {
	direEntries := []*log.Entry{}
	for _, entry := range hook.Entries {
		if entry.Level < log.InfoLevel {
			direEntries = append(direEntries, entry)
		}
	}
	return direEntries
}
