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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/openshift/cluster-operator/contrib/cmd/extract-co-jenkins-logs/util"
)

// Given a Jenkins URL, extracts logs of relevant containers to current or specified
// directory

// NewExtractLogsCommand returns a command that will extract logs
func NewExtractLogsCommand(outputDir, containersLog, dockerInfoLog string) *cobra.Command {
	return &cobra.Command{
		Use: "extract-co-jenkins-logs JENKINS_BUILD_URL",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) < 1 && (len(containersLog) == 0 || len(dockerInfoLog) == 0) {
				cmd.Usage()
				return
			}
			var jenkinsURL string
			if len(args) > 0 {
				jenkinsURL = args[0]
			}
			err := extractLogs(jenkinsURL, outputDir, containersLog, dockerInfoLog)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func main() {
	log.SetOutput(os.Stdout)
	var logLevel,
		outputDir,
		containersLog,
		dockerInfoLog string

	pflag.CommandLine.StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warning, error, fatal, panic")
	pflag.CommandLine.StringVar(&outputDir, "output-dir", "", "Output directory")
	pflag.CommandLine.StringVar(&containersLog, "containers-log", "", "File containing containers log")
	pflag.CommandLine.StringVar(&dockerInfoLog, "docker-info", "", "File containing docker.info")

	pflag.Parse()
	level, err := log.ParseLevel(logLevel)
	if err == nil {
		log.SetLevel(level)
	} else {
		log.Warningf("Invalid log level: %s. Defaulting to 'info'.\n", logLevel)
	}
	cmd := NewExtractLogsCommand(outputDir, containersLog, dockerInfoLog)
	cmd.Execute()
}

func extractLogs(jobURLString, outputDir, containersLog, dockerInfo string) error {
	// Download containers.log and docker.info artifacts
	var err error
	if len(containersLog) == 0 {
		containersLog, err = util.DownloadFile(jobURLString, "s3/download/generated/containers.log")
		if err != nil {
			return err
		}
	}
	if len(dockerInfo) == 0 {
		dockerInfo, err = util.DownloadFile(jobURLString, "s3/download/generated/docker.info")
		if err != nil {
			return err
		}
	}

	containers, err := util.ParseDockerInfo(dockerInfo)
	if err != nil {
		return err
	}

	if len(outputDir) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		ts := time.Now().Format("01-02-2006_15-04-05")
		outputDir = filepath.Join(wd, fmt.Sprintf("logs-%s", ts))
		if err = os.MkdirAll(outputDir, 0777); err != nil {
			return err
		}
	}

	err = util.ExtractContainerLogs(containers, containersLog, outputDir)
	if err != nil {
		return err
	}

	log.Infof("Logs extracted to %s", outputDir)
	return nil
}
