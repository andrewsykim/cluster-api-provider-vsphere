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

package vsphere

import (
	clustercommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/context"
	vsphereutils "sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/utils"
)

const ProviderName = "vsphere"

func init() {
	clustercommon.RegisterClusterProvisioner(ProviderName, &DeploymentClient{})
}

// Contains vsphere-specific deployment logic
// that implements ProviderDeployer interface at
// sigs.k8s.io/cluster-api/cmd/clusterctl/clusterdeployer/clusterdeployer.go
type DeploymentClient struct{}

func NewDeploymentClient() *DeploymentClient {
	return &DeploymentClient{}
}

func (d *DeploymentClient) GetIP(cluster *clusterv1.Cluster, _ *clusterv1.Machine) (string, error) {
	ctx, err := context.NewClusterContext(&context.ClusterContextParams{Cluster: cluster})
	if err != nil {
		return "", err
	}
	return vsphereutils.GetControlPlaneEndpoint(ctx)
}

func (d *DeploymentClient) GetKubeConfig(cluster *clusterv1.Cluster, _ *clusterv1.Machine) (string, error) {
	ctx, err := context.NewClusterContext(&context.ClusterContextParams{Cluster: cluster})
	if err != nil {
		return "", err
	}
	return vsphereutils.GetKubeConfig(ctx)
}
