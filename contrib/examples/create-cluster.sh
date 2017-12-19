#! /usr/bin/bash

oc process -f contrib/examples/cluster.yaml -p CLUSTER_NAME=$(whoami)-cluster | oc apply -f -
