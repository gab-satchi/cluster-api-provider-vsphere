// Copyright (c) 2019 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package vmoperator

import (
	"fmt"
	"github.com/pkg/errors"
	vmoprv1 "github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vmwarev1b1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context/vmware"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// RPService represents the ability to reconcile a VirtualMachineSetResourcePolicy via vmoperator
type RPService struct{}

// ReconcileResourcePolicy ensures that a VirtualMachineSetResourcePolicy exists for the cluster
// Returns the name of a policy if it exists, otherwise returns an error
func (s RPService) ReconcileResourcePolicy(ctx *vmware.ClusterContext) (string, error) {
	resourcePolicy, err := s.getVirtualMachineSetResourcePolicy(ctx)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return "", errors.Errorf("unexpected error in getting the Resource policy: %+v", err)
		}
		resourcePolicy, err = s.createVirtualMachineSetResourcePolicy(ctx)
		if err != nil {
			return "", errors.Errorf("failed to create Resource Policy: %+v", err)
		}
	}

	return resourcePolicy.Name, nil
}

func (s RPService) newVirtualMachineSetResourcePolicy(ctx *vmware.ClusterContext) *vmoprv1.VirtualMachineSetResourcePolicy {
	return &vmoprv1.VirtualMachineSetResourcePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.VSphereCluster.Namespace,
			Name:      ctx.VSphereCluster.Name,
		},
	}
}

func (s RPService) getVirtualMachineSetResourcePolicy(ctx *vmware.ClusterContext) (*vmoprv1.VirtualMachineSetResourcePolicy, error) {
	vmResourcePolicy := &vmoprv1.VirtualMachineSetResourcePolicy{}
	vmResourcePolicyName := client.ObjectKey{
		Namespace: ctx.VSphereCluster.Namespace,
		Name:      ctx.VSphereCluster.Name,
	}
	err := ctx.Client.Get(ctx, vmResourcePolicyName, vmResourcePolicy)
	return vmResourcePolicy, err
}

func (s RPService) createVirtualMachineSetResourcePolicy(ctx *vmware.ClusterContext) (*vmoprv1.VirtualMachineSetResourcePolicy, error) {
	vmResourcePolicy := s.newVirtualMachineSetResourcePolicy(ctx)

	_, err := ctrlutil.CreateOrUpdate(ctx, ctx.Client, vmResourcePolicy, func() error {
		vmResourcePolicy.Spec = vmoprv1.VirtualMachineSetResourcePolicySpec{
			ResourcePool: vmoprv1.ResourcePoolSpec{
				Name: ctx.VSphereCluster.Name,
			},
			Folder: vmoprv1.FolderSpec{
				Name: ctx.VSphereCluster.Name,
			},
			ClusterModules: []vmoprv1.ClusterModuleSpec{
				{
					GroupName: ControlPlaneVMClusterModuleGroupName,
				},
				{
					// TODO: assumes the name of the machine deployment
					GroupName: fmt.Sprintf("%s-workers-0", ctx.Cluster.Name),
				},
			},
		}
		// Ensure that the VirtualMachineSetResourcePolicy is owned by the VSphereCluster
		vmResourcePolicy.OwnerReferences = []metav1.OwnerReference{
			{
				Name:       ctx.VSphereCluster.Name,
				APIVersion: vmwarev1b1.GroupVersion.String(),
				Kind:       "VSphereCluster",
				UID:        ctx.VSphereCluster.UID,
			},
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return vmResourcePolicy, nil
}
