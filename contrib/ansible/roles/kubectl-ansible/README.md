Role Name
=========

kubectl-ansible

This role contains Ansible modules that wrap kubectl/oc client CLI functionality
for ease of use within Ansible playbooks and roles.

Install with:

`ansible-galaxy install git+git@github.com:dgoodwin/kubectl-ansible.git,master`


## Modules

### kubectl_apply

Exposes `kubectl apply` functionality to ansible. Will attempt to accurately
report changes, and supports custom resources and apiextensions.

The recommended approach for declarative management of Kubernetes config is
`kubectl apply`, which contains sophisticated logic for performing a three way
merge between an an objects incoming config, last applied config, and current
state.  This logic however is currently implemented client side, and thus not
available to callers of the API and non-Go languages. An effort is underway (as
of April 2018) to move this logic to the Kubernetes API server itself.

For more information see:

  * [Declarative Application Management In Kubernetes](https://docs.google.com/document/d/1cLPGweVEYrVqQvBLJg6sxV-TrE5Rm2MNOBA_cxZP2WU/edit?usp=sharing)

Ansible 2.5 recently released with preview modules [k8s_raw](https://docs.ansible.com/ansible/devel/modules/k8s_raw_module.html#k8s-raw-module) and [openshift_raw](https://docs.ansible.com/ansible/devel/modules/openshift_raw_module.html#openshift-raw-module). These modules however currently use a Python client library which relies on a fixed Python class existing for every API type you interact with. As such, they do not support custom resource definitions or apiextensions. An effort is underway to build a dynamic Python client which will then be integrated with these modules.

In the meantime, kubectl apply is the best tool we have available for
declarative on-going Kubernetes config management. This module is intended to
be a temporary solution while we wait, exposing the functionality kubectl
offers and making it easy to use within Ansible.  It's interface is as
consistent as possible with k8s_raw to help with eventually transitioning, and
when the above work completes this module can likely be abandoned.

### oc_process

Exposes `oc process` functionality for processing OpenShift templates. Result can then be created/applied with kubectl_apply or k8s_raw.


Requirements
------------

kubectl/oc binaries installed on the target system.

Role Variables
--------------

Dependencies
------------

Example Playbook
----------------

See examples/sample-playbook.yml for examples of everything you can do with this role and module.

License
-------

Apache License v2.0

Author Information
------------------

dgoodwin
