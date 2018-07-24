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
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

func ParseDockerInfo(fileName string) (map[string]string, error) {
	log.Debugf("Parsing docker.info file %s", fileName)
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := map[string]string{}
	scanner := bufio.NewScanner(file)
	inContainers := false
	var nameIndex int
	for scanner.Scan() {
		line := scanner.Text()
		if isContainersHeader(line) {
			nameIndex = getNameColumnIndex(line)
			inContainers = true
			continue
		}
		if inContainers {
			id, name := parseContainerLine(nameIndex, line)
			if len(id) > 0 {
				log.Debugf("Adding container id %s: %s", id, name)
				result[id] = name
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func isContainersHeader(str string) bool {
	return strings.HasPrefix(str, "CONTAINER ID")
}

func getNameColumnIndex(str string) int {
	return strings.Index(str, "NAMES")
}

func parseContainerLine(nameIndex int, line string) (string, string) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	id := parts[0]
	name := line[nameIndex:]
	return id, name
}
