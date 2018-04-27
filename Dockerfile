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

# Dockerfile to perform a full cluster-operator build from source.
#
# This Dockerfile is intended for performing in-cluster OpenShift builds. It differs
# from those in build/ in that it does not expect to compile the binary externally
# and add to the container, everything is built internally during docker build.
FROM golang:1.9

ENV PATH=/go/bin:$PATH GOPATH=/go

ADD . /go/src/github.com/openshift/cluster-operator

WORKDIR /go/src/github.com/openshift/cluster-operator
RUN NO_DOCKER=1 make build
ENTRYPOINT ["bin/cluster-operator"]

