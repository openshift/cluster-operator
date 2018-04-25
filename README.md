# cluster-operator

# Development Deployment

## Initial (One-Time) Setup

  * Install required packages:
    * Fedora: `sudo dnf install golang make docker ansible`
    * Mac OSX:
      * [Go](https://golang.org/doc/install#osx) 
      * [Ansible](http://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html#latest-releases-via-pip)
    * NOTE: Ansible 2.5+ is required to run our deployment playbooks.
  * Change docker to allow insecure pulls (required for `oc cluster up`):
    * Edit `/etc/sysconfig/docker`
    * Change `OPTIONS=` to include `--insecure-registry 172.30.0.0/16`
  * Enable and Start docker:
    * `sudo systemctl enable docker`
    * `sudo systemctl start docker`
  * Install the OpenShift and Kubernetes Python clients:
    * `sudo pip install kubernetes openshift`
  * Clone this repo to `$HOME/go/src/github.com/openshift/cluster-operator`
  * Get cfssl:
    * `go get -u github.com/cloudflare/cfssl/cmd/...`
  * Get the oc client binary
    * Fedora: Download a recent oc client binary from `origin/releases` (doesn't have to be 3.10):
      * https://github.com/openshift/origin/releases
      * Alternatively, you can also compile `oc` from source.
      * Note: It is recommended to put the `oc` binary somewhere in your path.
    * Mac OSX: [Minishift](https://github.com/minishift/minishift/releases) is the recommended development environment
  * Start an OpenShift cluster:
    * Fedora: `oc cluster up --image="docker.io/openshift/origin"`
    * Mac OSX: Follow the Minishift [Getting Started Guide](https://docs.openshift.org/latest/minishift/getting-started/index.html)
    * Note: Startup output will contain the URL to the web console for your openshift cluster, save this for later
  * Login to the OpenShift cluster as admin:
    * `oc login -u system:admin`
  * Grant admin rights to login to the web console
    * `oc adm policy add-cluster-role-to-user cluster-admin admin`
  * Ensure the following files are available on your local machine:
    * `$HOME/.aws/credentials` - your AWS credentials
    * `$HOME/.ssh/libra.pem` - the SSH private key to use for AWS


## Deploy / Re-deploy Cluster Operator

  * Compile the Go code and create the Cluster Operator images (both Go and Ansible):
    * Mac OSX only: `eval $(minishift docker-env)`
    * `make images`
  * Deploy cluster operator to the OpenShift cluster you are currently logged into. (see above for oc login example)
    * `ansible-playbook contrib/ansible/deploy-devel-playbook.yaml`
  * If your code/image changed, but the kubernetes config did not (which is usually the case), you should delete pods appropriately:
    * `oc delete pod -l app=cluster-operator-controller-manager`
    * Or if you would rather delete all pods including the apiserver (which seldom changes) and our etcd (which would delete your stored clusters): `oc delete pod --all -n openshift-cluster-operator`

## Creating a Test Cluster

  * `ansible-playbook contrib/ansible/create-cluster-playbook.yaml`
    * This will create a cluster named after your username in your current context's namespace, using a fake ClusterVersion. (no actual resources will be provisioned, the Ansible image used will just verify the playbook called exists, and return indicating success)
    * Specify `-e cluster_version` to use a real cluster version and provision an actual cluster in AWS. (see `oc get clusterversions -n openshift-cluster-operator` for list of the defaults we create)
    * Specify `-e cluster_name`, `-e cluster_namespace`, or other variables you can override as defined at the top of the playbook.
    * This command can be re-run to update the definition of the cluster and test how the cluster operator will respond to the change. (WARNING: do not try to change the name/namespace, as this will create a new cluster)

You can then check the provisioning status of your cluster by running `oc describe cluster <cluster_name>`

## Developing With OpenShift Ansible

The Cluster Operator uses its own Ansible image which layers our playbooks and roles on top of the upstream (https://github.com/openshift/openshift-ansible)[OpenShift Ansible] images. Typically our Ansible changes only require work in this repo. See the `build/cluster-operator-ansible` directory for the Dockerfile and playbooks we layer in.

To build the *cluster-operator-ansible* image you can just run `make images` normally.

**WARNING**: This image is built using OpenShift Ansible v3.10. This can be adjusted by specifying the CO_ANSIBLE_URL and CO_ANSIBLE_BRANCH environment variables to use a different branch/repository for the base openshift-ansible image.

You can run cluster-operator-ansible playbooks standalone by creating an inventory like:

```
[OSEv3:children]
masters
nodes
etcd

[OSEv3:vars]
ansible_become=true
ansible_ssh_user=centos
openshift_deployment_type=origin
openshift_release="3.10"
oreg_url=openshift/origin-${component}:v3.10.0
openshift_aws_ami=ami-833d37f9

[masters]

[etcd]

[nodes]
```

You can then run ansible with the above inventory file and your cluster ID:

`ansible-playbook -i ec2-hosts build/cluster-operator-ansible/playbooks/cluster-operator/node-config-daemonset.yml -e openshift_aws_clusterid=dgoodwin-cluster`

