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

FROM openshift/origin-ansible:v3.9

ARG CO_ANSIBLE_URL=https://github.com/openshift/openshift-ansible.git
ARG CO_ANSIBLE_BRANCH=release-3.9
ARG CLONE_LOCATION=/usr/share/ansible/openshift-ansible

USER root

# Git is no longer included in the origin-ansible base image. However,
# it is needed to build this image.
RUN yum -y install git

# WORKAROUND: Fix ansible 2.5.0 issue that causes playbooks to fail
# https://github.com/ansible/ansible/issues/37850
# Remove when ansible is updated to contain the fix
RUN yum -y install dmidecode
# End WORKAROUND

RUN rm -rf ${CLONE_LOCATION} && mkdir -p ${CLONE_LOCATION}
RUN git clone ${CO_ANSIBLE_URL} ${CLONE_LOCATION} && \
    cd ${CLONE_LOCATION} && \
    git checkout ${CO_ANSIBLE_BRANCH}

# WORKAROUND: Fix aws playbook to use the correct filter when 
# querying subnets from AWS.
# Remove when fix merges in v3.9 branch of openshift-ansible
RUN find /usr/share/ansible/openshift-ansible/roles/openshift_aws -type f -exec sed -e 's/availability_zone/availability-zone/g' -i {} \;
# End WORKAROUND

WORKDIR ${CLONE_LOCATION}

RUN mkdir ${CLONE_LOCATION}/playbooks/cluster-api-prep && ln -s ../../roles ./playbooks/cluster-api-prep
COPY playbooks/cluster-api-prep/ ${CLONE_LOCATION}/playbooks/cluster-api-prep

# WORKAROUND: /usr/local/bin/run doesn't handle INVENTORY_DIR when the
# files are symlinks (like when pointing to a configmap).
# Remove when openshift-ansible image has fix https://github.com/openshift/openshift-ansible/pull/9309
COPY run /usr/local/bin/run

# Remove any remnants of our playbooks in the upstream openshift-ansible image:
# TODO: This can be removed once we drop our directory from openshift-ansible.
RUN rm -rf /usr/share/ansible/openshift-ansible/playbooks/cluster-operator/

# Copy in our playbooks and roles:
COPY playbooks/cluster-operator ${CLONE_LOCATION}/playbooks/cluster-operator

# Overwrite role files:
# TODO: Remove this when we have no files to overwrite in roles
COPY roles.v3_9 ${CLONE_LOCATION}/roles

USER 1001:0
