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

package controllers

import (
	"fmt"
	"reflect"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/services/vmoperator"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/util"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/v1beta1"
	vmwarev1beta1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/record"
)

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;patch;update
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=vsphereclusteridentities,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=vsphereclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=vsphereclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

// AddClusterControllerToManager adds the cluster controller to the provided
// manager.
func AddClusterControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager, clusterControlledType client.Object) error {
	var supervisorBased bool
	switch clusterControlledType.(type) {
	case *infrav1.VSphereCluster:
		supervisorBased = false
	case *vmwarev1beta1.VSphereCluster:
		supervisorBased = true
	}
	clusterControlledTypeName := reflect.TypeOf(clusterControlledType).Elem().Name()

	var (
		clusterControlledTypeGVK = infrav1.GroupVersion.WithKind(clusterControlledTypeName)
		controllerNameShort      = fmt.Sprintf("%s-controller", strings.ToLower(clusterControlledTypeName))
		controllerNameLong       = fmt.Sprintf("%s/%s/%s", ctx.Namespace, ctx.Name, controllerNameShort)
	)
	if supervisorBased {
		controllerNameShort = fmt.Sprintf("%s-supervisor-controller", strings.ToLower(clusterControlledTypeName))
		controllerNameLong = fmt.Sprintf("%s/%s/%s", ctx.Namespace, ctx.Name, controllerNameShort)
	}

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Recorder:                 record.New(mgr.GetEventRecorderFor(controllerNameLong)),
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	if supervisorBased {
		// TODO: initialize services
		reconciler := supervisorClusterReconciler{
			ControllerContext: controllerContext,
			resourcePolicyService: vmoperator.RPService{},
		}
		return ctrl.NewControllerManagedBy(mgr).
			Named(controllerNameShort).
			For(clusterControlledType).
			Watches(
				&source.Kind{Type: &vmwarev1beta1.VSphereMachine{}},
				handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
					vsphereMachine, ok := o.(*vmwarev1beta1.VSphereMachine)
					if !ok {
						reconciler.Logger.Error(errors.New("did not get vspheremachine"), "got", fmt.Sprintf("%T", o))
						return nil
					}
					if !util.IsControlPlaneMachine(vsphereMachine) {
						reconciler.Logger.V(5).Info("rejecting vsphereCluster reconcile as not CP machine", "machineName", vsphereMachine.Name)
						return nil
					}
					// Only currently interested in updating Cluster from vsphereMachines with IP addresses
					if vsphereMachine.Status.IPAddr == "" {
						reconciler.Logger.V(5).Info("rejecting vsphereCluster reconcile as no IP address", "machineName", vsphereMachine.Name)
						return nil
					}

					cluster, err := util.GetVSphereClusterFromVSphereMachine(reconciler, reconciler.Client, vsphereMachine)
					if err != nil {
						reconciler.Logger.Error(err, "failed to get cluster", "machine", vsphereMachine.Name, "namespace", vsphereMachine.Namespace)
						return nil
					}

					// Can add further filters on Cluster state so that we don't keep reconciling Cluster
					reconciler.Logger.V(3).Info("triggering VSphereCluster reconcile from VSphereMachine", "machineName", vsphereMachine.Name)
					return []ctrl.Request{{
						NamespacedName: types.NamespacedName{
							Namespace: cluster.Namespace,
							Name:      cluster.Name,
						},
					}}
				}),
			).
			WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
			Complete(reconciler)
	}

	reconciler := clusterReconciler{ControllerContext: controllerContext}
	clusterToInfraFn := clusterutilv1.ClusterToInfrastructureMapFunc(clusterControlledTypeGVK)
	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(clusterControlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				requests := clusterToInfraFn(o)
				if requests == nil {
					return nil
				}

				c := &infrav1.VSphereCluster{}
				if err := reconciler.Client.Get(ctx, requests[0].NamespacedName, c); err != nil {
					reconciler.Logger.V(4).Error(err, "Failed to get VSphereCluster")
					return nil
				}

				if annotations.IsExternallyManaged(c) {
					reconciler.Logger.V(4).Info("VSphereCluster is externally managed, skipping mapping.")
					return nil
				}
				return requests
			}),
		).

		// Watch the infrastructure machine resources that belong to the control
		// plane. This controller needs to reconcile the infrastructure cluster
		// once a control plane machine has an IP address.
		Watches(
			&source.Kind{Type: &infrav1.VSphereMachine{}},
			handler.EnqueueRequestsFromMapFunc(reconciler.controlPlaneMachineToCluster),
		).
		// Watch the Vsphere deployment zone with the Server field matching the
		// server field of the VSphereCluster.
		Watches(
			&source.Kind{Type: &infrav1.VSphereDeploymentZone{}},
			handler.EnqueueRequestsFromMapFunc(reconciler.deploymentZoneToCluster),
		).
		// Watch a GenericEvent channel for the controlled resource.
		//
		// This is useful when there are events outside of Kubernetes that
		// should cause a resource to be synchronized, such as a goroutine
		// waiting on some asynchronous, external task to complete.
		Watches(
			&source.Channel{Source: ctx.GetGenericEventChannelFor(clusterControlledTypeGVK)},
			&handler.EnqueueRequestForObject{},
		).
		WithEventFilter(predicates.ResourceIsNotExternallyManaged(reconciler.Logger)).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Complete(reconciler)
}
