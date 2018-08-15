/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provision_aws

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openshift/library-go/pkg/crypto"
)

type ServingCert struct {
	Cert []byte
	Key  []byte
}

func (o *ProvisionClusterOptions) GenerateServingCert() (*ServingCert, error) {
	caConfig, err := crypto.MakeCAConfig(DefaultSignerName(), crypto.DefaultCACertificateLifetimeInDays)
	if err != nil {
		return nil, err
	}
	ca := crypto.CA{
		Config:          caConfig,
		SerialGenerator: &crypto.RandomSerialGenerator{},
	}
	hostNames := sets.NewString(
		fmt.Sprintf("%s.%s", o.Name, o.Namespace),
		fmt.Sprintf("%s.%s.svc", o.Name, o.Namespace),
	)
	server, err := ca.MakeServerCert(hostNames, crypto.DefaultCertificateLifetimeInDays)
	if err != nil {
		return nil, err
	}
	serverCert, serverKey, err := server.GetPEMBytes()
	if err != nil {
		return nil, err
	}
	return &ServingCert{
		Cert: serverCert,
		Key:  serverKey,
	}, nil
}

func DefaultSignerName() string {
	return fmt.Sprintf("%s@%d", "openshift-signer", time.Now().Unix())
}
