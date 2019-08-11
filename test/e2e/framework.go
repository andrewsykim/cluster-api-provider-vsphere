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
	"errors"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	vspherev1alpha1 "sigs.k8s.io/cluster-api-provider-vsphere/pkg/apis/vsphere/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capiclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

type Framework struct {
	CoreClient kubernetes.Interface
	CAPIClient capiclient.Interface

	managementCluster *v1alpha1.Cluster
	initialMachines   []v1alpha1.Machine
}

func NewFramework() (*Framework, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		return nil, errors.New("env var KUBECONFIG is empty, it should point to a kubeconfig file for your management cluster")
	}

	coreClient, err := kubeClientFromKubeConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	capiClient, err := capiClientFromKubeConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	f := &Framework{
		CoreClient: coreClient,
		CAPIClient: capiClient,
	}

	return f, nil
}

// PreStart checks to see if the prerequisites are met for a given cluster:
//  1) there is only 1 cluster resource in the cluster, for E2E tests we assume that is the management cluster
//  2) the cluster has at least 1 node
// PreStart also stores the initial cluster and machine resources into the Framework type.
func (f *Framework) PreStart() error {
	// for now, we assume a management cluster we are testing against only has itself represented as CRDs
	clusters, err := f.CAPIClient.ClusterV1alpha1().Clusters("default").List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(clusters.Items) != 1 {
		return fmt.Errorf("only 1 cluster should exist prior to starting E2E tests, found %d", len(clusters.Items))
	}

	f.managementCluster = &clusters.Items[0]

	machines, err := f.CAPIClient.ClusterV1alpha1().Machines("default").List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(machines.Items) == 0 {
		return errors.New("no machines in cluster")
	}

	f.initialMachines = machines.Items

	// list nodes to check client works
	nodes, err := f.CoreClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing nodes to validate cluster: %v", err)
	}

	if len(nodes.Items) == 0 {
		return errors.New("cluster has no nodes")
	}

	return nil
}

func (f *Framework) ManagementCluster() *v1alpha1.Cluster {
	return f.managementCluster
}

func (f *Framework) InitialMachines() []v1alpha1.Machine {
	return f.initialMachines
}

func (f *Framework) FirstMachine() *v1alpha1.Machine {
	return &f.initialMachines[0]
}

func kubeClientFromKubeConfig(kubeconfigPath string) (kubernetes.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func capiClientFromKubeConfig(kubeconfigPath string) (capiclient.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := capiclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, err
}

func VSphereSpecFromCluster(cluster *v1alpha1.Cluster) (*vspherev1alpha1.VsphereClusterProviderSpec, error) {
	providerSpec, err := vspherev1alpha1.GetClusterProviderSpec(cluster)
	if err != nil {
		return nil, err
	}

	return providerSpec, nil
}

func VSphereSpecFromMachine(machine *v1alpha1.Machine) (*vspherev1alpha1.VsphereMachineProviderSpec, error) {
	providerSpec, err := vspherev1alpha1.GetMachineProviderSpec(machine)
	if err != nil {
		return nil, err
	}

	return providerSpec, nil
}

func MarshalProviderSpecIntoCluster(cluster *v1alpha1.Cluster, providerSpec *vspherev1alpha1.VsphereClusterProviderSpec) error {
	rawExt, err := EncodeAsRawExtension(providerSpec)
	if err != nil {
		return err
	}

	cluster.Spec.ProviderSpec.Value = rawExt
	return nil
}

func MarshalProviderSpecIntoMachine(machine *v1alpha1.Machine, providerSpec *vspherev1alpha1.VsphereMachineProviderSpec) error {
	rawExt, err := EncodeAsRawExtension(providerSpec)
	if err != nil {
		return err
	}

	machine.Spec.ProviderSpec.Value = rawExt
	return nil
}

// EncodeAsRawExtension encodes a runtime.Object as a *runtime.RawExtension.
func EncodeAsRawExtension(in runtime.Object) (*runtime.RawExtension, error) {
	out := &runtime.RawExtension{}
	return out, runtime.Convert_runtime_Object_To_runtime_RawExtension(&in, out, nil)
}

func FilterMacAddrFromNetworkSpec(networkSpec vspherev1alpha1.NetworkSpec) vspherev1alpha1.NetworkSpec {
	for i := range networkSpec.Devices {
		networkSpec.Devices[i].MACAddr = ""
	}

	return networkSpec
}
