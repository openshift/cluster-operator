#!/bin/bash -e
# Create namespace for cluster-operator resources
oc create namespace cluster-operator || :

# Create ssl certs for api server
./contrib/apiserver-aggregation-tls-setup.sh

# Create cluster-operator resources
oc process -f contrib/examples/deploy.yaml -o yaml \
  -p SERVING_CA=$(base64 --wrap 0 ca.pem) \
  -p SERVING_CERT=$(base64 --wrap 0 apiserver.pem) \
  -p SERVING_KEY=$(base64 --wrap 0 apiserver-key.pem) \
| oc apply -f -

