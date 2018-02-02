#! /bin/bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# get-playbook-mock-runs queries the playbook-mock server for the playbook runs that
# it has logged.

set -o errexit

curl --silent http://$(oc get service -n cluster-operator playbook-mock -o json | jq -r .spec.clusterIP) | jq '.'
