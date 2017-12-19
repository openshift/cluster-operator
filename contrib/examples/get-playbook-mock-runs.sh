#! /usr/bin/bash
#
# get-playbook-mock-runs queries the playbook-mock server for the playbook runs that
# it has logged.
#

curl --silent http://$(oc get service -n cluster-operator playbook-mock -o json | jq -r .spec.clusterIP) | jq '.'
