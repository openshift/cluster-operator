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
#     KUBE_*          -> BOATSWAIN_*
#     kube            -> boatswain
#     KUBE_ROOT       -> ROOT
#     KUBE_GO_PACKAGE -> BOATSWAIN_GO_PACKAGE
#  added get_ldflags for use in Makefile

# -----------------------------------------------------------------------------
# Version management helpers.  These functions help to set, save and load the
# following variables:
#
#    BOATSWAIN_GIT_COMMIT - The git commit id corresponding to this
#          source code.
#    BOATSWAIN_GIT_TREE_STATE - "clean" indicates no changes since the git commit id
#        "dirty" indicates source code changes after the git commit id
#    BOATSWAIN_GIT_VERSION - "vX.Y" used to indicate the last release version.
#    BOATSWAIN_GIT_MAJOR - The major part of the version
#    BOATSWAIN_GIT_MINOR - The minor component of the version

# Grovels through git to set a set of env variables.
#
# If BOATSWAIN_GIT_VERSION_FILE, this function will load from that file instead of
# querying git.
boatswain::version::get_version_vars() {
  if [[ -n ${BOATSWAIN_GIT_VERSION_FILE-} ]]; then
    boatswain::version::load_version_vars "${BOATSWAIN_GIT_VERSION_FILE}"
    return
  fi

  local git=(git --work-tree "${ROOT}")

  if [[ -n ${BOATSWAIN_GIT_COMMIT-} ]] || BOATSWAIN_GIT_COMMIT=$("${git[@]}" rev-parse "HEAD^{commit}" 2>/dev/null); then
    if [[ -z ${BOATSWAIN_GIT_TREE_STATE-} ]]; then
      # Check if the tree is dirty.  default to dirty
      if git_status=$("${git[@]}" status --porcelain 2>/dev/null) && [[ -z ${git_status} ]]; then
        BOATSWAIN_GIT_TREE_STATE="clean"
      else
        BOATSWAIN_GIT_TREE_STATE="dirty"
      fi
    fi

    # Use git describe to find the version based on annotated tags.
    if [[ -n ${BOATSWAIN_GIT_VERSION-} ]] || BOATSWAIN_GIT_VERSION=$("${git[@]}" describe --tags --abbrev=14 "${BOATSWAIN_GIT_COMMIT}^{commit}" 2>/dev/null); then
      # This translates the "git describe" to an actual semver.org
      # compatible semantic version that looks something like this:
      #   v1.1.0-alpha.0.6+84c76d1142ea4d
      #
      # TODO: We continue calling this "git version" because so many
      # downstream consumers are expecting it there.
      DASHES_IN_VERSION=$(echo "${BOATSWAIN_GIT_VERSION}" | sed "s/[^-]//g")
      if [[ "${DASHES_IN_VERSION}" == "---" ]] ; then
        # We have distance to subversion (v1.1.0-subversion-1-gCommitHash)
        BOATSWAIN_GIT_VERSION=$(echo "${BOATSWAIN_GIT_VERSION}" | sed "s/-\([0-9]\{1,\}\)-g\([0-9a-f]\{14\}\)$/.\1\+\2/")
      elif [[ "${DASHES_IN_VERSION}" == "--" ]] ; then
        # We have distance to base tag (v1.1.0-1-gCommitHash)
        BOATSWAIN_GIT_VERSION=$(echo "${BOATSWAIN_GIT_VERSION}" | sed "s/-g\([0-9a-f]\{14\}\)$/+\1/")
      fi
      if [[ "${BOATSWAIN_GIT_TREE_STATE}" == "dirty" ]]; then
        # git describe --dirty only considers changes to existing files, but
        # that is problematic since new untracked .go files affect the build,
        # so use our idea of "dirty" from git status instead.
        BOATSWAIN_GIT_VERSION+="-dirty"
      fi


      # Try to match the "git describe" output to a regex to try to extract
      # the "major" and "minor" versions and whether this is the exact tagged
      # version or whether the tree is between two tagged versions.
      if [[ "${BOATSWAIN_GIT_VERSION}" =~ ^v([0-9]+)\.([0-9]+)(\.[0-9]+)?([-].*)?$ ]]; then
        BOATSWAIN_GIT_MAJOR=${BASH_REMATCH[1]}
        BOATSWAIN_GIT_MINOR=${BASH_REMATCH[2]}
        if [[ -n "${BASH_REMATCH[4]}" ]]; then
          BOATSWAIN_GIT_MINOR+="+"
        fi
      fi
    fi
  fi
}

# Saves the environment flags to $1
boatswain::version::save_version_vars() {
  local version_file=${1-}
  [[ -n ${version_file} ]] || {
    echo "!!! Internal error.  No file specified in boatswain::version::save_version_vars"
    return 1
  }

  cat <<EOF >"${version_file}"
BOATSWAIN_GIT_COMMIT='${BOATSWAIN_GIT_COMMIT-}'
BOATSWAIN_GIT_TREE_STATE='${BOATSWAIN_GIT_TREE_STATE-}'
BOATSWAIN_GIT_VERSION='${BOATSWAIN_GIT_VERSION-}'
BOATSWAIN_GIT_MAJOR='${BOATSWAIN_GIT_MAJOR-}'
BOATSWAIN_GIT_MINOR='${BOATSWAIN_GIT_MINOR-}'
EOF
}

# Loads up the version variables from file $1
boatswain::version::load_version_vars() {
  local version_file=${1-}
  [[ -n ${version_file} ]] || {
    echo "!!! Internal error.  No file specified in boatswain::version::load_version_vars"
    return 1
  }

  source "${version_file}"
}

boatswain::version::ldflag() {
  local key=${1}
  local val=${2}

  # If you update these, also update the list pkg/version/def.bzl.
  echo "-X ${BOATSWAIN_GO_PACKAGE}/pkg/version.${key}=${val}"
  echo "-X ${BOATSWAIN_GO_PACKAGE}/vendor/k8s.io/client-go/pkg/version.${key}=${val}"
}

# Prints the value that needs to be passed to the -ldflags parameter of go build
# in order to set the Kubernetes based on the git tree status.
# IMPORTANT: if you update any of these, also update the lists in
# pkg/version/def.bzl and hack/print-workspace-status.sh.
boatswain::version::ldflags() {
  boatswain::version::get_version_vars

  local buildDate=
  [[ -z ${SOURCE_DATE_EPOCH-} ]] || buildDate="--date=@${SOURCE_DATE_EPOCH}"
  local -a ldflags=($(boatswain::version::ldflag "buildDate" "$(date ${buildDate} -u +'%Y-%m-%dT%H:%M:%SZ')"))
  if [[ -n ${BOATSWAIN_GIT_COMMIT-} ]]; then
    ldflags+=($(boatswain::version::ldflag "gitCommit" "${BOATSWAIN_GIT_COMMIT}"))
    ldflags+=($(boatswain::version::ldflag "gitTreeState" "${BOATSWAIN_GIT_TREE_STATE}"))
  fi

  if [[ -n ${BOATSWAIN_GIT_VERSION-} ]]; then
    ldflags+=($(boatswain::version::ldflag "gitVersion" "${BOATSWAIN_GIT_VERSION}"))
  fi

  if [[ -n ${BOATSWAIN_GIT_MAJOR-} && -n ${BOATSWAIN_GIT_MINOR-} ]]; then
    ldflags+=(
      $(boatswain::version::ldflag "gitMajor" "${BOATSWAIN_GIT_MAJOR}")
      $(boatswain::version::ldflag "gitMinor" "${BOATSWAIN_GIT_MINOR}")
    )
  fi

  # The -ldflags parameter takes a single string, so join the output.
  echo "${ldflags[*]-}"
}

# called from Makefile
boatswain::version::get_ldflags() {
  export ROOT=$1
  export BOATSWAIN_GO_PACKAGE=$2
  boatswain::version::ldflags
}

boatswain::version::get_ldflags $1 $2
