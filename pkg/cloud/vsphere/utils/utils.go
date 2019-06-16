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

package utils

import (
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"os"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	capierr "sigs.k8s.io/cluster-api/pkg/controller/error"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/apis/vsphereproviderconfig/v1alpha1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/constants"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/context"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/services/certificates"
)

const (
	defaultBindPort = 6443
)

// byMachineCreatedTimestamp implements sort.Interface for []clusterv1.Machine
// based on the machine's creation timestamp.
type byMachineCreatedTimestamp []*clusterv1.Machine

func (a byMachineCreatedTimestamp) Len() int      { return len(a) }
func (a byMachineCreatedTimestamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byMachineCreatedTimestamp) Less(i, j int) bool {
	return a[i].CreationTimestamp.Before(&a[j].CreationTimestamp)
}

// GetControlPlaneEndpoint returns the control plane endpoint for the cluster.
// If a control plane endpoint was specified in the cluster configuration, then
// that value will be returned.
// Otherwise this function will return the endpoint of the first control plane
// node in the cluster that reports an IP address.
// If no control plane nodes have reported an IP address then this function
// returns an error.
func GetControlPlaneEndpoint(ctx *context.ClusterContext) (string, error) {

	if len(ctx.Cluster.Status.APIEndpoints) > 0 {
		controlPlaneEndpoint := net.JoinHostPort(ctx.Cluster.Status.APIEndpoints[0].Host, strconv.Itoa(ctx.Cluster.Status.APIEndpoints[0].Port))
		ctx.Logger.V(2).Info("got control plane endpoint from cluster APIEndpoints", "control-plane-endpoint", controlPlaneEndpoint)
		return controlPlaneEndpoint, nil
	}

	if controlPlaneEndpoint := ctx.ClusterConfig.ClusterConfiguration.ControlPlaneEndpoint; controlPlaneEndpoint != "" {
		ctx.Logger.V(2).Info("got control plane endpoint from cluster config", "control-plane-endpoint", controlPlaneEndpoint)
		return controlPlaneEndpoint, nil
	}

	if ctx.ClusterClient == nil {
		return "", errors.Errorf("cluster client is nil while searching for control plane endpoint for cluster %q", ctx)
	}

	controlPlaneMachines, err := ctx.GetControlPlaneMachines()
	if err != nil {
		return "", errors.Wrapf(err, "unable to get control plane machines while searching for control plane endpoint for cluster %q", ctx)
	}

	if len(controlPlaneMachines) == 0 {
		return "", errors.Wrapf(
			&capierr.RequeueAfterError{RequeueAfter: constants.RequeueAfterSeconds},
			"no control plane machines defined while searching for control plane endpoint for cluster %q", ctx)
	}

	// Sort the control plane machines so the first one created is always the
	// one used to provide the address for the control plane endpoint.
	sortedControlPlaneMachines := byMachineCreatedTimestamp(controlPlaneMachines)
	sort.Sort(sortedControlPlaneMachines)

	machine := sortedControlPlaneMachines[0]
	machineCtx, err := context.NewMachineContextFromClusterContext(ctx, machine)
	if err != nil {
		return "", errors.Wrapf(err, "unable to create machine context for %q while searching for control plane endpoint for cluster %q", machine.Name, ctx)
	}

	machineIPAddr := machineCtx.IPAddr()
	if machineIPAddr == "" {
		return "", errors.Wrapf(
			&capierr.RequeueAfterError{RequeueAfter: constants.RequeueAfterSeconds},
			"first control plane machine %q has not reported network address while searching for control plane endpoint for cluster %q", machineCtx, ctx)
	}

	// Check both the Init and Join config for the bind port. If the Join config
	// has a bind port that is different then use it.
	bindPort := GetAPIServerBindPort(machineCtx.MachineConfig)
	controlPlaneEndpoint := net.JoinHostPort(machineIPAddr, strconv.Itoa(int(bindPort)))

	machineCtx.Logger.V(2).Info("got control plane endpoint from machine config", "control-plane-endpoint", controlPlaneEndpoint)
	return controlPlaneEndpoint, nil
}

// GetAPIServerBindPort returns the APIServer bind port for a node
// joining the cluster.
func GetAPIServerBindPort(machineConfig *v1alpha1.VsphereMachineProviderConfig) int32 {
	bindPort := machineConfig.KubeadmConfiguration.Init.LocalAPIEndpoint.BindPort
	if cp := machineConfig.KubeadmConfiguration.Join.ControlPlane; cp != nil {
		if jbp := cp.LocalAPIEndpoint.BindPort; jbp != bindPort {
			bindPort = jbp
		}
	}
	if bindPort == 0 {
		bindPort = defaultBindPort
	}
	return bindPort
}

// GetKubeConfig returns the kubeconfig for the given cluster.
func GetKubeConfig(ctx *context.ClusterContext) (string, error) {
	cert, err := certificates.DecodeCertPEM(ctx.ClusterConfig.CAKeyPair.Cert)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode CA Cert")
	} else if cert == nil {
		return "", errors.New("certificate not found in config")
	}

	key, err := certificates.DecodePrivateKeyPEM(ctx.ClusterConfig.CAKeyPair.Key)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode private key")
	} else if key == nil {
		return "", errors.New("key not found in status")
	}

	controlPlaneEndpoint, err := GetControlPlaneEndpoint(ctx)
	if err != nil {
		return "", err
	}

	server := fmt.Sprintf("https://%s", controlPlaneEndpoint)

	cfg, err := certificates.NewKubeconfig(ctx.Cluster.Name, server, cert, key)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate a kubeconfig")
	}

	yaml, err := clientcmd.Write(*cfg)
	if err != nil {
		return "", errors.Wrap(err, "failed to serialize config to yaml")
	}

	return string(yaml), nil
}

// GetControlPlaneStatus returns a flag indicating whether or not the cluster
// is online.
// If the flag is true then the second return value is the cluster's control
// plane endpoint.
func GetControlPlaneStatus(ctx *context.ClusterContext) (bool, string, error) {
	controlPlaneEndpoint, err := getControlPlaneStatus(ctx)
	if err != nil {
		return false, "", errors.Wrapf(
			&capierr.RequeueAfterError{RequeueAfter: constants.RequeueAfterSeconds},
			"unable to get control plane status for cluster %q: %v", ctx, err)
	}
	return true, controlPlaneEndpoint, nil
}

func getControlPlaneStatus(ctx *context.ClusterContext) (string, error) {
	kubeClient, err := GetKubeClientForCluster(ctx)
	if err != nil {
		return "", err
	}
	if _, err := kubeClient.Nodes().List(metav1.ListOptions{}); err != nil {
		return "", errors.Wrapf(err, "unable to list nodes")
	}
	return GetControlPlaneEndpoint(ctx)
}

// GetKubeClientForCluster returns a Kubernetes client for the given cluster.
func GetKubeClientForCluster(ctx *context.ClusterContext) (corev1.CoreV1Interface, error) {
	kubeconfig, err := GetKubeConfig(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get kubeconfig for cluster %q", ctx)
	}
	clientConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, errors.Wrapf(err, "unable to create client config for cluster %q", ctx)
	}
	return corev1.NewForConfig(clientConfig)
}

// Just a temporary hack to grab a single range from the config.
func GetSubnet(netRange clusterv1.NetworkRanges) string {
	if len(netRange.CIDRBlocks) == 0 {
		return ""
	}
	return netRange.CIDRBlocks[0]
}

func CreateTempFile(contents string) (string, error) {
	tmpFile, err := ioutil.TempFile("", "")
	if err != nil {
		klog.Warningf("Error creating temporary file")
		return "", err
	}
	// For any error in this method, clean up the temp file
	defer func(pErr *error, path string) {
		if *pErr != nil {
			if err := os.Remove(path); err != nil {
				klog.Warningf("Error removing file '%s': %v", path, err)
			}
		}
	}(&err, tmpFile.Name())

	if _, err = tmpFile.Write([]byte(contents)); err != nil {
		klog.Warningf("Error writing to temporary file '%s'", tmpFile.Name())
		return "", err
	}
	if err = tmpFile.Close(); err != nil {
		return "", err
	}
	if err = os.Chmod(tmpFile.Name(), 0644); err != nil {
		klog.Warningf("Error setting file permission to 0644 for the temporary file '%s'", tmpFile.Name())
		return "", err
	}
	return tmpFile.Name(), nil
}

// ByteToGiB returns n/1024^3. The input must be an integer that can be
// appropriately divisible.
func ByteToGiB(n int64) int64 {
	return n / int64(math.Pow(1024, 3))
}

// GiBToByte returns n*1024^3.
func GiBToByte(n int64) int64 {
	return int64(n * int64(math.Pow(1024, 3)))
}

func IsValidUUID(str string) bool {
	_, err := uuid.Parse(str)
	return err == nil
}
