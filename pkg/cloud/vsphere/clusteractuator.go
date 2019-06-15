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
	"github.com/pkg/errors"

	// "k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterv1alpha1 "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	v1alpha1 "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions/cluster/v1alpha1"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/context"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/services/certificates"
	vsphereutils "sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/utils"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/record"
)

// ClusterActuator is responsible for maintaining the Cluster objects.
type ClusterActuator struct {
	client     clusterv1alpha1.ClusterV1alpha1Interface
	coreClient corev1.CoreV1Interface
	lister     v1alpha1.Interface
}

// NewClusterActuator creates the instance for the ClusterActuator
func NewClusterActuator(
	client clusterv1alpha1.ClusterV1alpha1Interface,
	coreClient corev1.CoreV1Interface,
	lister v1alpha1.Interface) (*ClusterActuator, error) {

	return &ClusterActuator{
		client:     client,
		coreClient: coreClient,
		lister:     lister,
	}, nil
}

// Reconcile will create or update the cluster
func (a *ClusterActuator) Reconcile(cluster *clusterv1.Cluster) (result error) {
	ctx, err := context.NewClusterContext(&context.ClusterContextParams{
		Cluster:    cluster,
		Client:     a.client,
		CoreClient: a.coreClient,
		Lister:     a.lister,
	})
	if err != nil {
		return err
	}

	defer func() {
		if result == nil {
			record.Eventf(ctx.Cluster, "ReconcileSuccess", "reconciled cluster %q", ctx)
		} else {
			record.Warnf(ctx.Cluster, "ReconcileFailure", "failed to reconcile cluster %q: %v", ctx, result)
		}
	}()

	ctx.Logger.V(2).Info("reconciling cluster")
	defer ctx.Patch(vsphereutils.GetControlPlaneStatus)

	// Ensure the PKI config is present or generated and then set the updated
	// clusterConfig back onto the cluster.
	if err := certificates.ReconcileCertificates(ctx); err != nil {
		return errors.Wrapf(err, "unable to reconcile certs while reconciling cluster %q", ctx)
	}

	return nil
}

// Delete will delete any cluster level resources for the cluster.
func (a *ClusterActuator) Delete(cluster *clusterv1.Cluster) (result error) {
	ctx, err := context.NewClusterContext(&context.ClusterContextParams{
		Cluster:    cluster,
		Client:     a.client,
		CoreClient: a.coreClient,
		Lister:     a.lister,
	})
	if err != nil {
		return err
	}

	defer func() {
		if result == nil {
			record.Eventf(ctx.Cluster, "DeleteSuccess", "deleted cluster %q", ctx)
		} else {
			record.Warnf(ctx.Cluster, "DeleteFailure", "failed to delete cluster %q: %v", ctx, result)
		}
	}()

	ctx.Logger.V(2).Info("deleting cluster")
	defer ctx.Patch(nil)

	return nil
}
