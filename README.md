# cluster-operator

# Development Setup

## Initial Setup

  * Install required packages:
    * Fedora: `dnf install golang make docker`
  * Change docker to allow insecure pulls (required for `oc cluster up`):
    * Edit `/etc/sysconfig/docker`
    * Change `OPTIONS=` to include `--insecure-registry 172.30.0.0/16`
  * Enable and Start docker:
    * `systemctl enable docker`
    * `systemctl start docker`
  * Clone this repo to `$HOME/go/src/github.com/openshift/cluster-operator`
  * Get cfssl:
    * `go get -u github.com/cloudflare/cfssl/cmd/...`
  * Download a recent oc client binary from `origin/releases` (doesn't have to be 3.8):
    * https://github.com/openshift/origin/releases
    * Alternatively, you can also compile `oc` from source.
    * Note: It is recommended to put the `oc` binary somewhere in your path.
  * Start an OpenShift cluster:
    * `oc cluster up --image="docker.io/openshift/origin" --version "v3.8.0-alpha.1"`
      * Check for newer 3.8 tags here if desired: https://hub.docker.com/r/openshift/origin/tags/
      * Watchout for "latest", it often will pick up older releases if they were the last to be built.
  * Login to the OpenShift cluster as admin:
    * `oc login -u system:admin`
  * Grant admin rights to login to the [WebUI](https://localhost:8443)
    * `oc adm policy add-cluster-role-to-user cluster-admin admin`


## Deploy / Re-deploy Cluster Operator
  * Compile the Go code and creates a docker image which is usable by the oc cluster.
    * `make images`
  * Idempotently deploy cluster operator to the OpenShift cluster.
    * `ansible-playbook contrib/ansible/deploy-devel.yaml`
  * If your image changed, but the kubernetes config did not, it is often required to delete all pods:
    * `oc delete pod --all -n cluster-operator`
