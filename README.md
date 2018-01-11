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
    * `oc cluster up --image="docker.io/openshift/origin" --version "latest"`
      * Check for newer 3.8 tags here if desired: https://hub.docker.com/r/openshift/origin/tags/
      * Watchout for "latest", it often will pick up older releases if they were the last to be built.
  * Login to the OpenShift cluster as admin:
    * `oc login -u system:admin`
  * Grant admin rights to login to the [WebUI](https://localhost:8443)
    * `oc adm policy add-cluster-role-to-user cluster-admin admin`


## Deploy / Re-deploy Cluster Operator
  * Compile the Go code and creates a docker image which is usable by the oc cluster.
    * `make images`
  * Before deploying, make sure the following files are available on your local machine:
    * `$HOME/.aws/credentials` - your AWS credentials
    * `$HOME/.ssh/libra.pem` - the SSH private key to use for AWS
  * Idempotently deploy cluster operator to the OpenShift cluster.
    * `ansible-playbook contrib/ansible/deploy-devel.yaml`
  * If your image changed, but the kubernetes config did not, it is often required to delete all pods:
    * `oc delete pod --all -n cluster-operator`

## Creating a Sample Cluster
  * `contrib/examples/create-cluster.sh`

## Testing Provisioning

Provisioning code is currently disabled to avoid doing anything unintentional
until we have a better handle on getting it working and cleaning up what it
creates.

To enable and test:

  1. Enable using real AWS by supplying the use_real_aws parameter to the contrib/ansible/deploy-devel.yaml playbook.
		* `ansible-playbook contrib/ansible/deploy-devel.yaml -e "use_real_aws=true"`
  1. Create a Cluster. Prefer a name for the Cluster that is prefaced with your username. This makes it easier to associate to you resources created in the AWS, making them easier to find and clean up. By default, if you are using the contrib/examples/create-cluster.sh script, the cluster will be prefaced with your username.
  1. You should see some action in the controller manager logs, and a provisioning job and associated pod.

