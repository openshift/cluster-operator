#!/bin/sh --

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

export BOATSWAIN_NAMESPACE=${BOATSWAIN_NAMESPACE:-boatswain}
BOATSWAIN_SERVICE_NAME=boatswain-apiserver

CA_NAME=ca

ALT_NAMES="\"${BOATSWAIN_SERVICE_NAME}.${BOATSWAIN_NAMESPACE}\",\"${BOATSWAIN_SERVICE_NAME}.${BOATSWAIN_NAMESPACE}.svc"\"

BOATSWAIN_CA_SETUP=boatswain-ca.json
cat > ${BOATSWAIN_CA_SETUP} << EOF
{
    "hosts": [ ${ALT_NAMES} ],
    "key": {
        "algo": "rsa",
        "size": 2048
    },
    "names": [
        {
            "C": "US",
            "L": "san jose",
            "O": "kube",
            "OU": "WWW",
            "ST": "California"
        }
    ]
}
EOF


cfssl genkey --initca ${BOATSWAIN_CA_SETUP} | cfssljson -bare ${CA_NAME}
# now the files 'ca.csr  ca-key.pem  ca.pem' exist

BOATSWAIN_CA_CERT=${CA_NAME}.pem
BOATSWAIN_CA_KEY=${CA_NAME}-key.pem

PURPOSE=server
echo '{"signing":{"default":{"expiry":"43800h","usages":["signing","key encipherment","'${PURPOSE}'"]}}}' > "${PURPOSE}-ca-config.json"

echo '{"CN":"'${BOATSWAIN_SERVICE_NAME}'","hosts":['${ALT_NAMES}'],"key":{"algo":"rsa","size":2048}}' \
 | cfssl gencert -ca=${BOATSWAIN_CA_CERT} -ca-key=${BOATSWAIN_CA_KEY} -config=server-ca-config.json - \
 | cfssljson -bare apiserver

export BOATSWAIN_SERVING_CA=${BOATSWAN_CA_CERT}
export BOATWSAIN_SERVING_CERT=apiserver.pem
export BOATSWAIN_SERVING_KEY=apiserver-key.pem

