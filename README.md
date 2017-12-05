# cluster-operator

# Development Setup

  1. Download recent 3.8 oc client binary: https://github.com/openshift/origin/releases/tag/v3.8.0-alpha.0 or compile from source.
  1. oc cluster up --version v3.8.0-alpha.1
    * Check for newer 3.8 tags here if desired: https://hub.docker.com/r/openshift/origin/tags/
    * Watchout for "latest", it often will pick up older releases if they were the last to be built.
  1. oc login -u system:admin
  1. make cluster-operator-image
    * TODO: currently seems to require setenforce 0
  1. ./contrib/examples/deploy.sh
    * Can be re-run to update Kubernetes config.
