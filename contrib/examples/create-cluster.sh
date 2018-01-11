#! /bin/bash

set -e

oc get secret aws-credentials -n cluster-operator -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -
oc get secret ssh-private-key -n cluster-operator -o yaml | sed -E '/(namespace:|annotations|last-applied-configuration:|selfLink|uid:|resourceVersion:)/d' | oc apply -f -

oc process -f contrib/examples/cluster.yaml -p CLUSTER_NAME=$(whoami)-cluster | oc apply -f -
