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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"

	log "github.com/sirupsen/logrus"
)

func joinURL(base, suffix string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, suffix)
	return u.String(), nil
}

func downloadURL(urlString string, out io.Writer) error {
	log.Debugf("downloading %s\n", urlString)
	resp, err := http.Get(urlString)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("error downloading %s: %s", urlString, resp.Status)
	}
	defer resp.Body.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func DownloadFile(base, suffix string) (string, error) {
	u, err := joinURL(base, suffix)
	if err != nil {
		return "", err
	}

	tmp, err := ioutil.TempFile("", "co-logs-")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	log.Debugf("Created temp file %s", tmp.Name())

	err = downloadURL(u, tmp)
	if err != nil {
		return "", err
	}

	return tmp.Name(), nil
}
