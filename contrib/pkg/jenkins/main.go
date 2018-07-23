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

package jenkins

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift/cluster-operator/contrib/pkg/jenkins/util"
)

// Given a Jenkins URL, extracts logs of relevant containers to current or specified
// directory

// NewExtractLogsCommand returns a command that will extract logs
func NewExtractLogsCommand() *cobra.Command {
	var logLevel,
		outputDir,
		containersLog,
		dockerInfoLog string

	cmd := &cobra.Command{
		Use:   "extract-jenkins-logs JENKINS_BUILD_URL",
		Short: "Extracts container logs from cluster operator jenkins e2e test",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) < 1 && (len(containersLog) == 0 || len(dockerInfoLog) == 0) {
				cmd.Usage()
				return
			}
			level, err := log.ParseLevel(logLevel)
			if err == nil {
				log.SetLevel(level)
			} else {
				log.Warningf("Invalid log level: %s. Defaulting to 'info'.\n", logLevel)
			}
			var jenkinsURL string
			if len(args) > 0 {
				jenkinsURL = args[0]
			}
			err = extractLogs(jenkinsURL, outputDir, containersLog, dockerInfoLog)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warning, error, fatal, panic")
	flags.StringVar(&outputDir, "output-dir", "", "Output directory")
	flags.StringVar(&containersLog, "containers-log", "", "File containing containers log")
	flags.StringVar(&dockerInfoLog, "docker-info", "", "File containing docker.info")
	return cmd
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
