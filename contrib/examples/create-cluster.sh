#! /bin/bash

# Copyright 2018 The Kubernetes Authors.
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

set -e

oc get secret aws-credentials -n cluster-operator -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -
oc get secret ssh-private-key -n cluster-operator -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -
oc get secret ssl-cert -n cluster-operator -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -

default_name="$(whoami)-cluster"
name="${1:-$default_name}"

if [ -e contrib/examples/${name}.yaml ]
then
	CLUSTER_YAML="${name}.yaml"
else
	CLUSTER_YAML="cluster.yaml"
fi

oc process -f contrib/examples/${CLUSTER_YAML} -p CLUSTER_NAME=${name} | oc apply -f -
