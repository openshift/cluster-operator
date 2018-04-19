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

: ${CLUSTER_OPERATOR_NAMESPACE:="openshift-cluster-operator"}

# Copy the secrets we use for AWS credentials, SSH key, and the cluster certificates to the namespace the cluster is being created in:
oc get secret aws-credentials -n "$CLUSTER_OPERATOR_NAMESPACE" -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -
oc get secret ssh-private-key -n "$CLUSTER_OPERATOR_NAMESPACE" -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -
oc get secret ssl-cert -n "$CLUSTER_OPERATOR_NAMESPACE" -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -


if [ -e contrib/examples/$(whoami)-cluster.yaml ]
then
	CLUSTER_YAML="$(whoami)-cluster.yaml"
else
	CLUSTER_YAML="cluster.yaml"
fi

if [ -z ${USE_REAL_AWS} ]
then
	: ${ANSIBLE_IMAGE:="fake-openshift-ansible:canary"}
	: ${ANSIBLE_IMAGE_PULL_POLICY:="Never"}
else
	: ${ANSIBLE_IMAGE:="cluster-operator-ansible:v3.10"}
	: ${ANSIBLE_IMAGE_PULL_POLICY:="Never"}
fi

CLUSTER_NAME=${CLUSTER_NAME:-$(whoami)-cluster}

oc process -f contrib/examples/${CLUSTER_YAML} \
	-p CLUSTER_NAME=${CLUSTER_NAME} \
	-p ANSIBLE_IMAGE=${ANSIBLE_IMAGE} \
	-p ANSIBLE_IMAGE_PULL_POLICY=${ANSIBLE_IMAGE_PULL_POLICY} \
	| oc apply -f -
