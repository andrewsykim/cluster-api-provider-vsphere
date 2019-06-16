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

package context

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	client "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/patch"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/apis/vsphereproviderconfig/v1alpha1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/constants"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/record"
)

// ClusterContextParams are the parameters needed to create a ClusterContext.
type ClusterContextParams struct {
	Context    context.Context
	Cluster    *clusterv1.Cluster
	Client     client.ClusterV1alpha1Interface
	CoreClient corev1.CoreV1Interface
	Logger     logr.Logger
}

// ClusterContext is a Go context used with a CAPI cluster.
type ClusterContext struct {
	context.Context
	Cluster       *clusterv1.Cluster
	ClusterCopy   *clusterv1.Cluster
	ClusterClient client.ClusterInterface
	ClusterConfig *v1alpha1.VsphereClusterProviderConfig
	ClusterStatus *v1alpha1.VsphereClusterProviderStatus
	Logger        logr.Logger
	client        client.ClusterV1alpha1Interface
	machineClient client.MachineInterface
	user          string
	pass          string
}

// NewClusterContext returns a new ClusterContext.
func NewClusterContext(params *ClusterContextParams) (*ClusterContext, error) {

	parentContext := params.Context
	if parentContext == nil {
		parentContext = context.Background()
	}

	var clusterClient client.ClusterInterface
	var machineClient client.MachineInterface
	if params.Client != nil {
		clusterClient = params.Client.Clusters(params.Cluster.Namespace)
		machineClient = params.Client.Machines(params.Cluster.Namespace)
	}

	clusterConfig, err := v1alpha1.ClusterConfigFromCluster(params.Cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load cluster provider config")
	}

	clusterStatus, err := v1alpha1.ClusterStatusFromCluster(params.Cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load cluster provider status")
	}

	logr := params.Logger
	if logr == nil {
		logr = klogr.New().WithName("default-logger")
	}
	logr = logr.WithName(params.Cluster.APIVersion).WithName(params.Cluster.Namespace).WithName(params.Cluster.Name)

	user := clusterConfig.VsphereUser
	pass := clusterConfig.VspherePassword
	if secretName := clusterConfig.VsphereCredentialSecret; secretName != "" {
		if params.CoreClient == nil {
			return nil, errors.Errorf("credential secret %q specified without core client", secretName)
		}
		logr.V(4).Info("fetching vsphere credentials", "secret-name", secretName)
		secret, err := params.CoreClient.Secrets(params.Cluster.Namespace).Get(secretName, metav1.GetOptions{})
		if err != nil {
			return nil, errors.Wrapf(err, "error reading secret %q for cluster %s/%s", secretName, params.Cluster.Namespace, params.Cluster.Name)
		}
		user2, userOk := secret.Data[constants.VsphereUserKey]
		pass2, passOk := secret.Data[constants.VspherePasswordKey]
		if !userOk || !passOk {
			return nil, errors.Wrapf(err, "improperly formatted secret %q for cluster %s/%s", secretName, params.Cluster.Namespace, params.Cluster.Name)
		}
		user, pass = string(user2), string(pass2)
		logr.V(2).Info("found vSphere credentials")
	}

	return &ClusterContext{
		Context:       parentContext,
		Cluster:       params.Cluster,
		ClusterCopy:   params.Cluster.DeepCopy(),
		ClusterClient: clusterClient,
		ClusterConfig: clusterConfig,
		ClusterStatus: clusterStatus,
		Logger:        logr,
		client:        params.Client,
		machineClient: machineClient,
		user:          user,
		pass:          pass,
	}, nil
}

// Strings returns ClusterNamespace/ClusterName
func (c *ClusterContext) String() string {
	return fmt.Sprintf("%s/%s", c.Cluster.Namespace, c.Cluster.Name)
}

// User returns the username used to access the vSphere endpoint.
func (c *ClusterContext) User() string {
	return c.user
}

// Pass returns the password used to access the vSphere endpoint.
func (c *ClusterContext) Pass() string {
	return c.pass
}

// CanLogin returns a flag indicating whether the cluster config has
// enough information to login to the vSphere endpoint.
func (c *ClusterContext) CanLogin() bool {
	return c.ClusterConfig.VsphereServer != "" && c.user != ""
}

// GetMachineClient returns a new Machine client for this cluster.
func (c *ClusterContext) GetMachineClient() client.MachineInterface {
	if c.client != nil {
		return c.client.Machines(c.Cluster.Namespace)
	}
	return nil
}

// GetMachines gets the machines in the cluster.
func (c *ClusterContext) GetMachines() ([]*clusterv1.Machine, error) {
	labelSet := labels.Set(map[string]string{
		clusterv1.MachineClusterLabelName: c.Cluster.Name,
	})
	list, err := c.machineClient.List(metav1.ListOptions{LabelSelector: labelSet.AsSelector().String()})
	if err != nil {
		return nil, err
	}
	machines := make([]*clusterv1.Machine, len(list.Items))
	for i := range list.Items {
		machines[i] = &list.Items[i]
	}
	return machines, nil
}

// GetControlPlaneMachines returns the control plane machines for the cluster.
func (c *ClusterContext) GetControlPlaneMachines() ([]*clusterv1.Machine, error) {
	machines, err := c.GetMachines()
	if err != nil {
		return nil, err
	}
	controlPlaneMachines := []*clusterv1.Machine{}
	for _, machine := range machines {
		if role := GetMachineRole(machine); role == ControlPlaneRole {
			controlPlaneMachines = append(controlPlaneMachines, machine)
		}
	}
	return controlPlaneMachines, nil
}

// GetControlPlaneStatusFunc returns a flag indicating whether the control plane
// is online. If true, the control plane's endpoint will also be returned.
type GetControlPlaneStatusFunc func(ctx *ClusterContext) (online bool, controlPlaneEndpoint string, err error)

// Patch updates the cluster on the API server.
func (c *ClusterContext) Patch(getControlPlaneStatus GetControlPlaneStatusFunc) error {
	ext, err := v1alpha1.EncodeClusterSpec(c.ClusterConfig)
	if err != nil {
		return errors.Wrapf(err, "failed encoding cluster spec for cluster %q", c)
	}
	newStatus, err := v1alpha1.EncodeClusterStatus(c.ClusterStatus)
	if err != nil {
		return errors.Wrapf(err, "failed encoding cluster status for cluster %q", c)
	}
	ext.Object = nil
	newStatus.Object = nil

	c.Cluster.Spec.ProviderSpec.Value = ext

	// Build a patch and marshal that patch to something the client will
	// understand.
	p, err := patch.NewJSONPatch(c.ClusterCopy, c.Cluster)
	if err != nil {
		return errors.Wrapf(err, "failed to create new JSONPatch for cluster %q", c)
	}

	// Do not update Cluster if nothing has changed
	if len(p) != 0 {
		pb, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return errors.Wrapf(err, "failed to json marshal patch for cluster %q", c)
		}

		c.Logger.V(1).Info("patching cluster")
		c.Logger.V(6).Info("generated json patch for cluster", "json-patch", string(pb))

		result, err := c.ClusterClient.Patch(c.Cluster.Name, types.JSONPatchType, pb)
		//result, err := c.ClusterClient.Update(c.Cluster)
		if err != nil {
			record.Warnf(c.Cluster, updateFailure, "failed to update cluster config %q: %v", c, err)
			return errors.Wrapf(err, "failed to patch cluster %q", c)
		}

		record.Eventf(c.Cluster, updateSuccess, "updated cluster config %q", c)

		// Keep the resource version updated so the status update can succeed
		c.Cluster.ResourceVersion = result.ResourceVersion
	}

	if getControlPlaneStatus == nil {
		getControlPlaneStatus = controlPlaneOffline
	}

	// If the cluster is online then update the cluster's APIEndpoints
	// to include the control plane endpoint.
	if ok, controlPlaneEndpoint, _ := getControlPlaneStatus(c); ok {
		host, szPort, err := net.SplitHostPort(controlPlaneEndpoint)
		if err != nil {
			return errors.Wrapf(err, "unable to get host/port for control plane endpoint %q for cluster %q", controlPlaneEndpoint, c)
		}
		port, err := strconv.Atoi(szPort)
		if err != nil {
			return errors.Wrapf(err, "unable to get parse host and port for control plane endpoint %q for cluster %q", controlPlaneEndpoint, c)
		}
		if len(c.Cluster.Status.APIEndpoints) == 0 || (c.Cluster.Status.APIEndpoints[0].Host != host && c.Cluster.Status.APIEndpoints[0].Port != port) {
			c.Cluster.Status.APIEndpoints = []clusterv1.APIEndpoint{
				clusterv1.APIEndpoint{
					Host: host,
					Port: port,
				},
			}
			c.ClusterStatus.Ready = true
		}
	}
	c.Cluster.Status.ProviderStatus = newStatus

	if !reflect.DeepEqual(c.Cluster.Status, c.ClusterCopy.Status) {
		c.Logger.V(1).Info("updating cluster status")
		if _, err := c.ClusterClient.UpdateStatus(c.Cluster); err != nil {
			record.Warnf(c.Cluster, updateFailure, "failed to update cluster status for cluster %q: %v", c, err)
			return errors.Wrapf(err, "failed to update cluster status for cluster %q", c)
		}
		record.Eventf(c.Cluster, updateSuccess, "updated cluster status for cluster %q", c)
	}

	return nil
}

func controlPlaneOffline(ctx *ClusterContext) (bool, string, error) {
	return false, "", nil
}
