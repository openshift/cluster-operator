## **This project is deprecated.** Please see [https://github.com/openshift/hive](https://github.com/openshift/hive).

# cluster-operator

# Development Deployment

## Initial (One-Time) Setup

  * Install required packages:
    * Fedora: `sudo dnf install golang make docker ansible`
    * Mac OSX:
      * [Go](https://golang.org/doc/install#osx)
      * [Ansible](http://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html#latest-releases-via-pip)
  * Change docker to allow insecure pulls (required for `oc cluster up`) and change the log driver to json-file (more reliable):
    * Edit `/etc/sysconfig/docker`
    * Change `OPTIONS=` to include `--insecure-registry 172.30.0.0/16 --log-driver=json-file`
  * Enable and Start docker:
    * `sudo systemctl enable --now docker`
  * Install the OpenShift and Kubernetes Python clients:
    * `sudo pip install kubernetes openshift`
  * Install python SELinux libraries
    * Fedora 27: `sudo dnf install libselinux-python`
    * Fedora 28 (and later): `sudo dnf install python2-libselinux`
  * Clone this repo to `$HOME/go/src/github.com/openshift/cluster-operator`
  * Get cfssl:
    * `go get -u github.com/cloudflare/cfssl/cmd/...`
  * Get the oc client binary
    * Fedora: Download a recent oc client binary from `origin/releases` (doesn't have to be 3.10):
      * https://github.com/openshift/origin/releases
      * Alternatively, you can also compile `oc` from source.
      * Note: It is recommended to put the `oc` binary somewhere in your path.
    * Mac OSX: [Minishift](https://github.com/minishift/minishift/releases) is the recommended development environment
  * Create a `kubectl` symlink to the `oc` binary (if you don't already have it). This is necessary for the `kubectl_apply` ansible module to work.
    * Note: It is recommended to put the `kubectl` symlink somewhere in your path.
    * `ln -s oc kubectl`
  * Start an OpenShift cluster:
    * Fedora: `oc cluster up --image="docker.io/openshift/origin"`
    * Mac OSX: Follow the Minishift [Getting Started Guide](https://docs.openshift.org/latest/minishift/getting-started/index.html)
    * Note: Startup output will contain the URL to the web console for your openshift cluster, save this for later
  * Login to the OpenShift cluster as system:admin:
    * `oc login -u system:admin`
  * Create an "admin" account with cluster-admin role which you can use to login to the [WebUI](https://localhost:8443) or with oc:
    * `oc adm policy add-cluster-role-to-user cluster-admin admin`
  * Login to the OpenShift cluster as a normal admin account:
    * `oc login -u admin -p password`
  * Ensure the following files are available on your local machine:
    * `$HOME/.aws/credentials` - your AWS credentials, default section will be used but can be overridden by vars when running the create cluster playbook.
    * `$HOME/.ssh/libra.pem` - the SSH private key to use for AWS


## Deploy / Re-deploy Cluster Operator

| **WARNING** |
| ---- |
| By default when using `deploy-devel-playbook.yml` to deploy cluster operator, fake images will be used. This means that no actual cluster will be created. If you want to create a real cluster, pass `-e fake_deployment=false` to the playbook invocation. |

  * Deploy cluster operator to the OpenShift cluster you are currently logged into. (see above for oc login instructions above)
    * `ansible-playbook contrib/ansible/deploy-devel-playbook.yml`
    * This creates an OpenShift BuildConfig and ImageStream for the cluster-operator image. (which does not yet exist)
  * `deploy-devel-playbook.yml` automatically kicks off an image compile. To re-compile and push a new image:
    * If you would just like to deploy Cluster Operator from the latest code in git:
      * `oc start-build cluster-operator -n openshift-cluster-operator`
    * If you are a developer and would like to quickly compile code locally and deploy to your cluster:
      * Mac OSX only: `eval $(minishift docker-env)`
      * `NO_DOCKER=1 make images`
        * This will compile the go code locally, and build both cluster-operator and cluster-operator-ansible images.
      * `make integrated-registry-push`
        * This will attempt to get your current OpenShift whoami token, login to the integrated cluster registry, and push your local images.
	* This will immediately trigger a deployment now that the images are available.
    * Re-run these steps to deploy new code as often as you like, once the push completes the ImageStream will trigger a new deployment.

## Creating a Test Cluster

  * `ansible-playbook contrib/ansible/create-cluster-playbook.yml`
    * This will create a cluster named after your username in your current context's namespace, using a fake ClusterVersion. (no actual resources will be provisioned, the Ansible image used will just verify the playbook called exists, and return indicating success)
    * Specify `-e cluster_name`, `-e cluster_namespace`, or other variables you can override as defined at the top of the playbook.
    * This command can be re-run to update the definition of the cluster and test how the cluster operator will respond to the change. (WARNING: do not try to change the name/namespace, as this will create a new cluster)

You can then check the provisioning status of your cluster by running `oc describe cluster <cluster_name>`

## Developing Cluster Operator Controllers Locally

If you are actively working on controller code you can save some time by compiling and running locally:

  * Run the deploy playbooks normally.
  * Disable your controller in the cluster-operator-controller-manager DeploymentConfig using *one* of the below methods:
    * Scale everything down: `oc scale -n openshift-cluster-operator --replicas=0 dc/cluster-operator-controller-manager`
    * Disable just your controller: `oc edit -n openshift-cluster-operator DeploymentConfig cluster-operator-controller-manager` and add an argument for --controllers=-disableme or --controllers=c1,c2,c3 for just the controllers you want.
    * Delete it entirely: `oc delete -n openshift-cluster-operator DeploymentConfig cluster-operator-controller-manager`
  * `make build`
    * On Mac you may need to instead build a Darwin binary with: `go install ./cmd/cluster-operator`
  * `bin/cluster-operator controller-manager --log-level debug --k8s-kubeconfig ~/.kube/config`
    * You can adjust the controllers run with `--controllers clusterapi,machineset,etc`. Use --help to see the full list.

## Developing With OpenShift Ansible

The Cluster Operator uses its own Ansible image which layers our playbooks and roles on top of the upstream [OpenShift Ansible](https://github.com/openshift/openshift-ansible) images. Typically our Ansible changes only require work in this repo. See the `build/cluster-operator-ansible` directory for the Dockerfile and playbooks we layer in.

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

## Maintenance

### Use of kubectl_ansible and oc_process modules
We're using the Cluster Operator deployment Ansible as a testing ground for the
kubectl-ansible modules that wrap apply and oc process. These roles are
vendored in similar to how golang works using a tool called
[gogitit](https://github.com/dgoodwin/gogitit/). The required gogitit manifest
and cache are committed, but only the person updating the vendored code needs
to install the tool or worry about the manifest. For everyone else the roles
are just available normally and this allows us to not require developers to
periodically re-run ansible-galaxy install.

Updating the vendored code can be done with:

```
$ cd contrib/ansible/
$ gogitit sync
```

### Roles Template Duplication

For OpenShift CI our roles template, which we do not have permissions to apply ourselves, had to be copied to https://github.com/openshift/release/blob/master/projects/cluster-operator/cluster-operator-roles-template.yaml. Our copy in this repo is authoritative, we need to remember to copy the file and submit a PR, and request someone run the make target for us whenever the auth/roles definitions change.

## Utilities

You can build the development utilities binary `coutil` by running: `make coutil`. Once built, the binary will be placed in `bin/coutil`.
Utilities are subcommands under `coutil` and include: 

- `aws-actuator-test` - allows invoking AWS actuator actions (create, update, delete) without requiring a cluster to be present.
- `extract-jenkins-logs` - extracts container logs from a cluster operator e2e run, given a Jenkins job URL
- `playbook-mock` - used by the fake-ansible image to track invocations of ansible by cluster operator controllers
- `wait-for-apiservice` - given the name of an API service, waits for the API service to be functional.
- `wait-for-cluster-ready` - waits for a cluster operator ClusterDeployment to be provisioned and functional, reporting on its progress along the way.
