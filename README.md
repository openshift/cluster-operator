# cluster-operator

# Development Setup

## Initial Setup

  * get cfssl: `go get -u github.com/cloudflare/cfssl/cmd/...`
  * Download recent 3.8 oc client binary or compile from source.
    * TODO: link needed
  * `oc cluster up --version v3.8.0-alpha.1`
    * Check for newer 3.8 tags here if desired: https://hub.docker.com/r/openshift/origin/tags/
    * Watchout for "latest", it often will pick up older releases if they were the last to be built.
  * `oc login -u system:admin`
  * `oc adm policy add-cluster-role-to-user cluster-admin admin`
    * Allows you to login to the [WebUI](https://localhost:8443) as 'admin'.

## Deploy / Re-deploy Cluster Operator
  * `make images`
    * Compiles the Go code and creates a docker image which is usable by the oc cluster.
  * `ansible-playbook contrib/ansible/deploy-devel.yaml`
    * Idempotently deploys the application config to the cluster.
  * `oc delete pod --all -n cluster-operator`
    * Often required if your image changed, but the kubernetes config did not.

