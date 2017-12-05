#!/bin/bash

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

set -o errexit

# Note: this was copied from hack/lib/version.sh and then adapted
# Changes from original:
#   variables renamed:
#     KUBE_*          -> CLUSTER_OPERATOR_*
#     kube            -> clusteroperator
#     KUBE_ROOT       -> ROOT
#     KUBE_GO_PACKAGE -> CLUSTER_OPERATOR_GO_PACKAGE
#  added get_ldflags for use in Makefile

# -----------------------------------------------------------------------------
# Version management helpers.  These functions help to set, save and load the
# following variables:
#
#    CLUSTER_OPERATOR_GIT_COMMIT - The git commit id corresponding to this
#          source code.
#    CLUSTER_OPERATOR_GIT_TREE_STATE - "clean" indicates no changes since the git commit id
#        "dirty" indicates source code changes after the git commit id
#    CLUSTER_OPERATOR_GIT_VERSION - "vX.Y" used to indicate the last release version.
#    CLUSTER_OPERATOR_GIT_MAJOR - The major part of the version
#    CLUSTER_OPERATOR_GIT_MINOR - The minor component of the version

# Grovels through git to set a set of env variables.
#
# If CLUSTER_OPERATOR_GIT_VERSION_FILE, this function will load from that file instead of
# querying git.
clusteroperator::version::get_version_vars() {
  if [[ -n ${CLUSTER_OPERATOR_GIT_VERSION_FILE-} ]]; then
    clusteroperator::version::load_version_vars "${CLUSTER_OPERATOR_GIT_VERSION_FILE}"
    return
  fi

  local git=(git --work-tree "${ROOT}")

  if [[ -n ${CLUSTER_OPERATOR_GIT_COMMIT-} ]] || CLUSTER_OPERATOR_GIT_COMMIT=$("${git[@]}" rev-parse "HEAD^{commit}" 2>/dev/null); then
    if [[ -z ${CLUSTER_OPERATOR_GIT_TREE_STATE-} ]]; then
      # Check if the tree is dirty.  default to dirty
      if git_status=$("${git[@]}" status --porcelain 2>/dev/null) && [[ -z ${git_status} ]]; then
        CLUSTER_OPERATOR_GIT_TREE_STATE="clean"
      else
        CLUSTER_OPERATOR_GIT_TREE_STATE="dirty"
      fi
    fi

    # Use git describe to find the version based on annotated tags.
    if [[ -n ${CLUSTER_OPERATOR_GIT_VERSION-} ]] || CLUSTER_OPERATOR_GIT_VERSION=$("${git[@]}" describe --tags --abbrev=14 "${CLUSTER_OPERATOR_GIT_COMMIT}^{commit}" 2>/dev/null); then
      # This translates the "git describe" to an actual semver.org
      # compatible semantic version that looks something like this:
      #   v1.1.0-alpha.0.6+84c76d1142ea4d
      #
      # TODO: We continue calling this "git version" because so many
      # downstream consumers are expecting it there.
      DASHES_IN_VERSION=$(echo "${CLUSTER_OPERATOR_GIT_VERSION}" | sed "s/[^-]//g")
      if [[ "${DASHES_IN_VERSION}" == "---" ]] ; then
        # We have distance to subversion (v1.1.0-subversion-1-gCommitHash)
        CLUSTER_OPERATOR_GIT_VERSION=$(echo "${CLUSTER_OPERATOR_GIT_VERSION}" | sed "s/-\([0-9]\{1,\}\)-g\([0-9a-f]\{14\}\)$/.\1\+\2/")
      elif [[ "${DASHES_IN_VERSION}" == "--" ]] ; then
        # We have distance to base tag (v1.1.0-1-gCommitHash)
        CLUSTER_OPERATOR_GIT_VERSION=$(echo "${CLUSTER_OPERATOR_GIT_VERSION}" | sed "s/-g\([0-9a-f]\{14\}\)$/+\1/")
      fi
      if [[ "${CLUSTER_OPERATOR_GIT_TREE_STATE}" == "dirty" ]]; then
        # git describe --dirty only considers changes to existing files, but
        # that is problematic since new untracked .go files affect the build,
        # so use our idea of "dirty" from git status instead.
        CLUSTER_OPERATOR_GIT_VERSION+="-dirty"
      fi


      # Try to match the "git describe" output to a regex to try to extract
      # the "major" and "minor" versions and whether this is the exact tagged
      # version or whether the tree is between two tagged versions.
      if [[ "${CLUSTER_OPERATOR_GIT_VERSION}" =~ ^v([0-9]+)\.([0-9]+)(\.[0-9]+)?([-].*)?$ ]]; then
        CLUSTER_OPERATOR_GIT_MAJOR=${BASH_REMATCH[1]}
        CLUSTER_OPERATOR_GIT_MINOR=${BASH_REMATCH[2]}
        if [[ -n "${BASH_REMATCH[4]}" ]]; then
          CLUSTER_OPERATOR_GIT_MINOR+="+"
        fi
      fi
    fi
  fi
}

# Saves the environment flags to $1
clusteroperator::version::save_version_vars() {
  local version_file=${1-}
  [[ -n ${version_file} ]] || {
    echo "!!! Internal error.  No file specified in clusteroperator::version::save_version_vars"
    return 1
  }

  cat <<EOF >"${version_file}"
CLUSTER_OPERATOR_GIT_COMMIT='${CLUSTER_OPERATOR_GIT_COMMIT-}'
CLUSTER_OPERATOR_GIT_TREE_STATE='${CLUSTER_OPERATOR_GIT_TREE_STATE-}'
CLUSTER_OPERATOR_GIT_VERSION='${CLUSTER_OPERATOR_GIT_VERSION-}'
CLUSTER_OPERATOR_GIT_MAJOR='${CLUSTER_OPERATOR_GIT_MAJOR-}'
CLUSTER_OPERATOR_GIT_MINOR='${CLUSTER_OPERATOR_GIT_MINOR-}'
EOF
}

# Loads up the version variables from file $1
clusteroperator::version::load_version_vars() {
  local version_file=${1-}
  [[ -n ${version_file} ]] || {
    echo "!!! Internal error.  No file specified in clusteroperator::version::load_version_vars"
    return 1
  }

  source "${version_file}"
}

clusteroperator::version::ldflag() {
  local key=${1}
  local val=${2}

  # If you update these, also update the list pkg/version/def.bzl.
  echo "-X ${CLUSTER_OPERATOR_GO_PACKAGE}/pkg/version.${key}=${val}"
  echo "-X ${CLUSTER_OPERATOR_GO_PACKAGE}/vendor/k8s.io/client-go/pkg/version.${key}=${val}"
}

# Prints the value that needs to be passed to the -ldflags parameter of go build
# in order to set the Kubernetes based on the git tree status.
# IMPORTANT: if you update any of these, also update the lists in
# pkg/version/def.bzl and hack/print-workspace-status.sh.
clusteroperator::version::ldflags() {
  clusteroperator::version::get_version_vars

  local buildDate=
  [[ -z ${SOURCE_DATE_EPOCH-} ]] || buildDate="--date=@${SOURCE_DATE_EPOCH}"
  local -a ldflags=($(clusteroperator::version::ldflag "buildDate" "$(date ${buildDate} -u +'%Y-%m-%dT%H:%M:%SZ')"))
  if [[ -n ${CLUSTER_OPERATOR_GIT_COMMIT-} ]]; then
    ldflags+=($(clusteroperator::version::ldflag "gitCommit" "${CLUSTER_OPERATOR_GIT_COMMIT}"))
    ldflags+=($(clusteroperator::version::ldflag "gitTreeState" "${CLUSTER_OPERATOR_GIT_TREE_STATE}"))
  fi

  if [[ -n ${CLUSTER_OPERATOR_GIT_VERSION-} ]]; then
    ldflags+=($(clusteroperator::version::ldflag "gitVersion" "${CLUSTER_OPERATOR_GIT_VERSION}"))
  fi

  if [[ -n ${CLUSTER_OPERATOR_GIT_MAJOR-} && -n ${CLUSTER_OPERATOR_GIT_MINOR-} ]]; then
    ldflags+=(
      $(clusteroperator::version::ldflag "gitMajor" "${CLUSTER_OPERATOR_GIT_MAJOR}")
      $(clusteroperator::version::ldflag "gitMinor" "${CLUSTER_OPERATOR_GIT_MINOR}")
    )
  fi

  # The -ldflags parameter takes a single string, so join the output.
  echo "${ldflags[*]-}"
}

# called from Makefile
clusteroperator::version::get_ldflags() {
  export ROOT=$1
  export CLUSTER_OPERATOR_GO_PACKAGE=$2
  clusteroperator::version::ldflags
}

clusteroperator::version::get_ldflags $1 $2
