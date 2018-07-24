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

package util

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
)

func ExtractContainerLogs(containers map[string]string, logFilename, outputDir string) error {
	log.Debugf("Extracting container logs from %s. Output directory: %s\n", logFilename, outputDir)
	file, err := os.Open(logFilename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inFile := false
	var currentFile io.WriteCloser
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "==>"):
			if inFile {
				currentFile.Close()
				currentFile = nil
			}
			fileName := parseFileLine(line, containers)
			if len(fileName) > 0 {
				currentFile, err = os.Create(filepath.Join(outputDir, fileName))
				if err != nil {
					return err
				}
				inFile = true
			}
		case len(line) == 0:
			if inFile {
				currentFile.Close()
				currentFile = nil
				inFile = false
			}
		default:
			if inFile {
				logLine := parseLogLine(line)
				if len(logLine) > 0 {
					fmt.Fprintf(currentFile, "%s", logLine)
				}
			}
		}
	}
	if currentFile != nil {
		currentFile.Close()
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	return nil
}

var containerIdRE = regexp.MustCompile("\\/containers\\/([^/]+)\\/")

func parseFileLine(line string, containers map[string]string) string {
	parts := containerIdRE.FindStringSubmatch(line)
	if len(parts) < 2 {
		return ""
	}
	containerId := parts[1][:12]
	log.Debugf("Found start of container log for container id: %s", containerId)
	containerName, ok := containers[containerId]
	if !ok {
		log.Debugf("Did not find container id %s in containers map", containerId)
		return ""
	}
	// split the containerName in its parts
	parts = strings.Split(containerName, "_")
	// skip non-k8s containers
	if parts[0] != "k8s" {
		log.Debugf("Skipping container %s because it's not run by Kubernetes", containerName)
		return ""
	}
	// skip pause containers
	if parts[1] == "POD" {
		log.Debugf("Skipping container %s because it's a pause pod", containerName)
		return ""
	}
	container := parts[1]
	pod := parts[2]
	namespace := parts[3]

	log.Debugf("Found container with ns: %s, pod: %s, container: %s", namespace, pod, container)

	return fmt.Sprintf("%s_%s_%s.log", namespace, pod, container)
}

type LogLine struct {
	Log string `json:"log"`
}

func parseLogLine(line string) string {
	logLine := LogLine{}
	json.Unmarshal([]byte(line), &logLine)
	return logLine.Log
}
