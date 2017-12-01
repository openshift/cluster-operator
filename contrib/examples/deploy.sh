# Create namespace for boatswain resources
oc create namespace boatswain

# Create ssl certs for api server
./contrib/apiserver-aggregation-tls-setup.sh

# Create boatswain resources
oc process -f contrib/examples/deploy.yaml -o yaml \
  -p SERVING_CA=$(base64 --wrap 0 ca.pem) \
  -p SERVING_CERT=$(base64 --wrap 0 apiserver.pem) \
  -p SERVING_KEY=$(base64 --wrap 0 apiserver-key.pem) \
| oc create -f -

