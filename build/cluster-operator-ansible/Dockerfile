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

FROM openshift/origin-ansible:latest

ARG CO_ANSIBLE_URL=https://github.com/openshift/openshift-ansible.git
ARG CO_ANSIBLE_BRANCH=master
ARG CLONE_LOCATION=/usr/share/ansible/openshift-ansible

USER root

RUN yum -y install git
# Update the AWS client code so that operations like ec2_image work
RUN pip install awscli botocore boto3 -U

RUN rm -rf ${CLONE_LOCATION} && mkdir -p ${CLONE_LOCATION}
RUN git clone ${CO_ANSIBLE_URL} ${CLONE_LOCATION} && \
    cd ${CLONE_LOCATION} && \
    git checkout ${CO_ANSIBLE_BRANCH}

WORKDIR ${CLONE_LOCATION}

RUN mkdir ${CLONE_LOCATION}/playbooks/cluster-api-prep && ln -s ../../roles ./playbooks/cluster-api-prep
COPY playbooks/cluster-api-prep/ ${CLONE_LOCATION}/playbooks/cluster-api-prep

# Remove any remnants of our playbooks in the upstream openshift-ansible image:
# TODO: This can be removed once we drop our directory from openshift-ansible.
RUN rm -rf /usr/share/ansible/openshift-ansible/playbooks/cluster-operator/

# Copy in our playbooks and roles:
COPY playbooks/cluster-operator ${CLONE_LOCATION}/playbooks/cluster-operator

# Copy overrides for playbooks needed in openshift-ansible v3.11 and above
COPY playbooks.v3_11/cluster-operator ${CLONE_LOCATION}/playbooks/cluster-operator

USER 1001:0
