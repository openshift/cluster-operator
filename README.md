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
  * Create a temporary ssh key secret for use when connecting to hosts with ansible:
    * `kubectl create -n cluster-operator secret generic ssh-private-key --from-file=ssh-privatekey=/home/dgoodwin/libra.pem`


## Deploy / Re-deploy Cluster Operator
  * Compile the Go code and creates a docker image which is usable by the oc cluster.
    * `make images`
  * Idempotently deploy cluster operator to the OpenShift cluster.
    * `ansible-playbook contrib/ansible/deploy-devel.yaml`
  * If your image changed, but the kubernetes config did not, it is often required to delete all pods:
    * `oc delete pod --all -n cluster-operator`

## Testing Provisioning

Provisioning code is currently disabled to avoid doing anything unintentional
until we have a better handle on getting it working and cleaning up what it
creates.

To enable and test:

  1. Uncomment the correct Command in the pod definition in pkg/ansible/runner.py.
  1. `cp ./contrib/examples/cluster.yaml ./contrib/examples/mycluster.yaml`
  1. Edit mycluster.yaml and change the name to your username. This will allow you to find objects created in the AWS account to clean up.
  1. `kubectl create -n cluster-operator -f ./contrib/examples/dgoodwin-cluster.yaml`
    * Ability to create the cluster in any namespace will be coming shortly but for now, must be cluster-operator.
  1. You should see some action in the controller manager logs, and a provisioning job and associated pod.

