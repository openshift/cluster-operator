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

package main

// Tests individual AWS actuator actions. This is meant to be executed
// in a machine that has access to AWS either as an instance with the right role
// or creds in ~/.aws/credentials

import (
	"bytes"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"

	coapi "github.com/openshift/cluster-operator/pkg/api"
	cov1 "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	awsutil "github.com/aws/aws-sdk-go/aws"
	"github.com/openshift/cluster-operator/pkg/clusterapi/aws"
)

const instanceIDAnnotation = "cluster-operator.openshift.io/aws-instance-id"

func usage() {
	fmt.Printf("Usage: %s CLUSTER-NAME\n\n", os.Args[0])
}

func strptr(str string) *string {
	return &str
}

func createClusterMachine(name string) error {
	cluster, machine := testClusterAPIResources(name)
	actuator := aws.NewActuator(nil, nil, log.WithField("example", "create-machine"), "us-east-1c")
	result, err := actuator.CreateMachine(cluster, machine)
	if err != nil {
		return err
	}
	fmt.Printf("Machine creation was successful! InstanceID: %s\n", *result.InstanceId)
	return nil
}

func deleteClusterMachine(instanceID string) error {
	_, machine := testClusterAPIResources("any")
	machine.Annotations = map[string]string{
		instanceIDAnnotation: instanceID,
	}
	actuator := aws.NewActuator(nil, nil, log.WithField("example", "delete-machine"), "us-east-1c")
	err := actuator.DeleteMachine(machine)
	if err != nil {
		return err
	}
	fmt.Printf("Machine delete operation was successful.\n")
	return nil
}

func clusterMachineExists(instanceID string) error {
	cluster, machine := testClusterAPIResources("any")
	machine.Annotations = map[string]string{
		instanceIDAnnotation: instanceID,
	}
	actuator := aws.NewActuator(nil, nil, log.WithField("example", "delete-machine"), "us-east-1c")
	exists, err := actuator.Exists(cluster, machine)
	if err != nil {
		return err
	}
	fmt.Printf("Instance exists result: %v\n", exists)
	return nil
}

func newActuatorTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aws-actuator-test",
		Short: "Test for Cluster API AWS actuator",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "create CLUSTER-NAME",
		Short: "Create machine instance for specified cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.Usage()
				return nil
			}
			return createClusterMachine(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "delete INSTANCE-ID",
		Short: "Delete machine instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.Usage()
				return nil
			}
			return deleteClusterMachine(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "exists INSTANCE-ID",
		Short: "Determine if machine instance exists",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.Usage()
				return nil
			}
			return clusterMachineExists(args[0])
		},
	})
	return cmd
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	cmd := newActuatorTestCommand()
	err := cmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}

func testClusterAPIResources(name string) (*clusterv1.Cluster, *clusterv1.Machine) {
	clusterProviderConfigSpec := &cov1.AWSClusterProviderConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "clusteroperator.openshift.io/v1alpha1",
			Kind:       "AWSClusterProviderConfig",
		},
		Hardware: cov1.AWSClusterSpec{
			Defaults: &cov1.MachineSetAWSHardwareSpec{
				InstanceType: "t2.xlarge",
			},
			SSHSecret: corev1.LocalObjectReference{
				Name: "ssh-private-key",
			},
			SSHUser: "centos",
			SSLSecret: corev1.LocalObjectReference{
				Name: "ssl-cert",
			},
			Region:      "us-east-1",
			KeyPairName: "libra",
		},
	}

	machineSetProviderConfigSpec := &cov1.MachineSetProviderConfigSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "clusteroperator.openshift.io/v1alpha1",
			Kind:       "MachineSetProviderConfigSpec",
		},
		MachineSetSpec: cov1.MachineSetSpec{
			VMImage: cov1.VMImage{
				AWSImage: awsutil.String("ami-0e8468df91f4e8b6c"),
			},
			MachineSetConfig: cov1.MachineSetConfig{
				NodeType: cov1.NodeTypeCompute,
				Size:     1,
				Hardware: &cov1.MachineSetHardwareSpec{
					AWS: &cov1.MachineSetAWSHardwareSpec{
						InstanceType: "t2.xlarge",
					},
				},
			},
			ClusterHardware: cov1.ClusterHardwareSpec{
				AWS: clusterProviderConfigSpec.Hardware.DeepCopy(),
			},
		},
	}

	// Serialize cluster version and add it as an annotation to the MachineSet
	//	coMachineSet.Annotations = map[string]string{
	//		"cluster-operator.openshift.io/cluster-version": serializeCOResource(&coVersion),
	//	}

	// Now define cluster-api resources
	cluster := clusterv1.Cluster{}
	cluster.Name = name
	cluster.Spec.ProviderConfig.Value = &runtime.RawExtension{
		Raw: []byte(serializeCOResource(clusterProviderConfigSpec)),
	}

	machine := clusterv1.Machine{}
	machine.Name = name + "-compute-machine-1"
	machine.Spec.ProviderConfig.Value = &runtime.RawExtension{
		Raw: []byte(serializeCOResource(machineSetProviderConfigSpec)),
	}

	return &cluster, &machine
}

func serializeCOResource(object runtime.Object) string {
	serializer := jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, coapi.Scheme, coapi.Scheme, false)
	encoder := coapi.Codecs.EncoderForVersion(serializer, cov1.SchemeGroupVersion)
	buffer := &bytes.Buffer{}
	encoder.Encode(object, buffer)
	return buffer.String()
}
