/*
Copyright 2019 The Kubernetes Authors.

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

package e2e

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	vspherev1alpha1 "sigs.k8s.io/cluster-api-provider-vsphere/pkg/apis/vsphere/v1alpha1"
	"sigs.k8s.io/cluster-api-provider-vsphere/test/e2e"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

func Test_createTargetClusterOnlyControlPlane(t *testing.T) {
	f, err := e2e.NewFramework()
	if err != nil {
		t.Fatalf("error initializing framework: %v", err)
	}

	err = f.PreStart()
	if err != nil {
		t.Fatalf("error with E2E prestart: %v", err)
	}

	clusterProviderSpec, err := e2e.VSphereSpecFromCluster(f.ManagementCluster())
	if err != nil {
		t.Fatalf("error getting cluster provider spec: %v", err)
	}

	machineProviderSpec, err := e2e.VSphereSpecFromMachine(f.FirstMachine())
	if err != nil {
		t.Fatalf("error getting machine provider spec: %v", err)
	}

	// TODO: generate namespace name with capv-e2e prefix?
	namespace := "capv-e2e-" + utilrand.String(5)
	_, err = f.CoreClient.CoreV1().Namespaces().Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	})
	if err != nil {
		t.Fatalf("error creating namespace %q, err: %v", namespace, err)
	}

	serviceCIDR := "100.64.0.0/13"
	clusterCIDR := "100.96.0.0/11"

	tests := []struct {
		name                string
		cluster             *v1alpha1.Cluster
		clusterProviderSpec *vspherev1alpha1.VsphereClusterProviderSpec
		machine             *v1alpha1.Machine
		machineProviderSpec *vspherev1alpha1.VsphereMachineProviderSpec
	}{
		{
			name: "same spec as management, only 1 control plane machine",
			cluster: &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cap-e2e-with-one-control-plane",
					Namespace: namespace,
				},
				Spec: v1alpha1.ClusterSpec{
					ClusterNetwork: v1alpha1.ClusterNetworkingConfig{
						Services: v1alpha1.NetworkRanges{
							CIDRBlocks: []string{serviceCIDR},
						},
						Pods: v1alpha1.NetworkRanges{
							CIDRBlocks: []string{clusterCIDR},
						},
					},
					ProviderSpec: v1alpha1.ProviderSpec{},
				},
			},
			clusterProviderSpec: &vspherev1alpha1.VsphereClusterProviderSpec{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "vsphere.cluster.k8s.io/v1alpha1",
					Kind:       "VsphereClusterProviderSpec",
				},
				Server:                     clusterProviderSpec.Server,
				Username:                   clusterProviderSpec.Username,
				Password:                   clusterProviderSpec.Password,
				CloudProviderConfiguration: clusterProviderSpec.CloudProviderConfiguration,
			},
			machine: &v1alpha1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "capv-e2e-with-one-control-plane-machine",
					Namespace: namespace,
					Labels: map[string]string{
						"cluster.k8s.io/cluster-name": "cap-e2e-with-one-control-plane",
					},
				},
				Spec: v1alpha1.MachineSpec{
					ProviderSpec: v1alpha1.ProviderSpec{},
					Versions:     f.FirstMachine().Spec.Versions,
				},
			},
			machineProviderSpec: &vspherev1alpha1.VsphereMachineProviderSpec{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "vsphere.cluster.k8s.io/v1alpha1",
					Kind:       "VsphereMachineProviderSpec",
				},
				Datacenter: machineProviderSpec.Datacenter,
				Network:    e2e.FilterMacAddrFromNetworkSpec(machineProviderSpec.Network),
				Template:   machineProviderSpec.Template,
				NumCPUs:    machineProviderSpec.NumCPUs,
				MemoryMiB:  machineProviderSpec.MemoryMiB,
				DiskGiB:    machineProviderSpec.DiskGiB,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e2e.MarshalProviderSpecIntoCluster(test.cluster, test.clusterProviderSpec)
			e2e.MarshalProviderSpecIntoMachine(test.machine, test.machineProviderSpec)

			_, err := f.CAPIClient.ClusterV1alpha1().Clusters(namespace).Create(test.cluster)
			if err != nil {
				t.Fatalf("error creating target cluster: %v", err)
			}

			_, err = f.CAPIClient.ClusterV1alpha1().Machines(namespace).Create(test.machine)
			if err != nil {
				t.Fatalf("error creating machine: %v", err)
			}
		})
	}

}
