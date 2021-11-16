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
	goctx "context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"
	vmoprv1 "github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/v1beta1"
	vmwarev1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context/vmware"
	inframanager "sigs.k8s.io/cluster-api-provider-vsphere/pkg/manager"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/record"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/services"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/services/vmoperator"
	infrautilv1 "sigs.k8s.io/cluster-api-provider-vsphere/pkg/util"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=vspheremachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=vspheremachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// AddMachineControllerToManager adds the machine controller to the provided
// manager.
func AddMachineControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager, controlledType client.Object) error {
	var supervisorBased bool
	switch controlledType.(type) {
	case *infrav1.VSphereMachine:
		supervisorBased = false
	case *vmwarev1.VSphereMachine:
		supervisorBased = true
	default:
		return errors.New(fmt.Sprintf("unexpected type %s for VSphereMachine controller", reflect.TypeOf(controlledType)))
	}

	var (
		controlledTypeName  = reflect.TypeOf(controlledType).Elem().Name()
		controlledTypeGVK   = infrav1.GroupVersion.WithKind(controlledTypeName)
		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(controlledTypeName))
		controllerNameLong  = fmt.Sprintf("%s/%s/%s", ctx.Namespace, ctx.Name, controllerNameShort)
	)

	if supervisorBased {
		controllerNameShort = fmt.Sprintf("%s-supervisor-controller", strings.ToLower(controlledTypeName))
		controllerNameLong = fmt.Sprintf("%s/%s/%s", ctx.Namespace, ctx.Name, controllerNameShort)
	}

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Recorder:                 record.New(mgr.GetEventRecorderFor(controllerNameLong)),
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(clusterutilv1.MachineToInfrastructureMapFunc(controlledTypeGVK)),
		).
		// Watch a GenericEvent channel for the controlled resource.
		//
		// This is useful when there are events outside of Kubernetes that
		// should cause a resource to be synchronized, such as a goroutine
		// waiting on some asynchronous, external task to complete.
		Watches(
			&source.Channel{Source: ctx.GetGenericEventChannelFor(controlledTypeGVK)},
			&handler.EnqueueRequestForObject{},
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles})

	r := machineReconciler{
		ControllerContext: controllerContext,
		VMService:         &services.VimMachineService{},
		supervisorBased:   supervisorBased,
	}

	if supervisorBased {
		// Watch any VirtualMachine resources owned by this VSphereMachine
		builder.Owns(&vmoprv1.VirtualMachine{})
		r.VMService = &vmoperator.VmopMachineService{}
		networkProvider, err := inframanager.GetNetworkProvider(ctx, mgr.GetConfig())
		if err != nil {
			return errors.Wrap(err, "failed to create a network provider")
		}
		r.networkProvider = networkProvider
	} else {
		// Watch any VSphereVM resources owned by the controlled type.
		builder.Watches(&source.Kind{Type: &infrav1.VSphereVM{}}, &handler.EnqueueRequestForOwner{OwnerType: controlledType, IsController: false})
	}

	c, err := builder.Build(r)
	if err != nil {
		return err
	}

	if !supervisorBased {
		err = c.Watch(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(r.clusterToVSphereMachines),
			predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldCluster := e.ObjectOld.(*clusterv1.Cluster)
					newCluster := e.ObjectNew.(*clusterv1.Cluster)
					return oldCluster.Spec.Paused && !newCluster.Spec.Paused
				},
				CreateFunc: func(e event.CreateEvent) bool {
					if _, ok := e.Object.GetAnnotations()[clusterv1.PausedAnnotation]; !ok {
						return false
					}
					return true
				},
			})
		if err != nil {
			return err
		}
	}
	return nil
}

type machineReconciler struct {
	*context.ControllerContext
	VMService       services.VSphereMachineService
	networkProvider services.NetworkProvider
	supervisorBased bool
}

// nolint:gocognit
// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r machineReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	if r.supervisorBased {
		logger := r.Logger.WithName(req.Namespace).WithName(req.Name)
		logger.V(3).Info("Starting Reconcile VSphereMachine")
		// Fetch the VSphereMachine instance.
		vsphereMachine := &vmwarev1.VSphereMachine{}
		if err := r.Client.Get(r, req.NamespacedName, vsphereMachine); err != nil {
			if apierrors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}

		// Fetch the CAPI Machine.
		machine, err := clusterutilv1.GetOwnerMachine(r, r.Client, vsphereMachine.ObjectMeta)
		if err != nil {
			return reconcile.Result{}, err
		}
		var cluster *clusterv1.Cluster
		if machine != nil {
			cluster = r.fetchCAPICluster(machine, vsphereMachine)
		}

		// Fetch the VSphereCluster
		vsphereCluster := &vmwarev1.VSphereCluster{}
		var vsphereClusterErr error
		if cluster != nil {
			vsphereClusterName := client.ObjectKey{
				Namespace: vsphereMachine.Namespace,
				Name:      cluster.Spec.InfrastructureRef.Name,
			}

			vsphereClusterErr = r.Client.Get(r, vsphereClusterName, vsphereCluster)
		}
		// Create the patch helper.
		patchHelper, err := patch.NewHelper(vsphereMachine, r.Client)
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(
				err,
				"failed to init patch helper for %s %s/%s",
				vsphereMachine.GroupVersionKind(),
				vsphereMachine.Namespace,
				vsphereMachine.Name)
		}
		// Create the machine context for this request.
		clusterContext := &vmware.ClusterContext{
			ControllerContext: r.ControllerContext,
			Cluster:           cluster,
			VSphereCluster:    vsphereCluster,
		}

		machineContext := &vmware.SupervisorMachineContext{
			BaseMachineContext: &context.BaseMachineContext{
				ControllerContext: r.ControllerContext,
				Cluster:           cluster,
				Machine:           machine,
				Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
			},
			ClusterContext: clusterContext,
			PatchHelper:    patchHelper,
			VSphereCluster: vsphereCluster,
			VSphereMachine: vsphereMachine,
		}
		defer func() {
			r.Recorder.EmitEvent(vsphereMachine, "Reconcile", reterr, true)
			if err := machineContext.Patch(); err != nil {
				machineContext.Logger.Error(err, "patch failed", "machine", machineContext.String())
				if reterr == nil {
					reterr = err
				}
			}
		}()

		// Handle deleted machines
		if !vsphereMachine.ObjectMeta.DeletionTimestamp.IsZero() {
			return r.reconcileDelete(machineContext)
		}

		if vsphereClusterErr != nil {
			r.Logger.Info("unable to retrieve VSphereCluster", "error", vsphereClusterErr)
			return reconcile.Result{}, nil
		}

		if machine == nil {
			r.Logger.V(2).Info("waiting on Machine controller to set OwnerRef on infra machine")
			return reconcile.Result{}, nil
		}

		// Handle non-deleted machines
		return r.reconcileNormal(machineContext)
	}

	// infrav1.VSphereMachine reconciliation
	// Get the VSphereMachine resource for this request.
	vsphereMachine := &infrav1.VSphereMachine{}
	if err := r.Client.Get(r, req.NamespacedName, vsphereMachine); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("VSphereMachine not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the CAPI Machine.
	machine, err := clusterutilv1.GetOwnerMachine(r, r.Client, vsphereMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		r.Logger.Info("Waiting for Machine Controller to set OwnerRef on VSphereMachine")
		return reconcile.Result{}, nil
	}

	cluster := r.fetchCAPICluster(machine, vsphereMachine)
	if cluster == nil {
		return reconcile.Result{}, nil
	}

	// Fetch the VSphereCluster
	vsphereCluster := &infrav1.VSphereCluster{}
	vsphereClusterName := client.ObjectKey{
		Namespace: vsphereMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(r, vsphereClusterName, vsphereCluster); err != nil {
		r.Logger.Info("Waiting for VSphereCluster")
		return reconcile.Result{}, nil
	}
	// Create the patch helper.
	patchHelper, err := patch.NewHelper(vsphereMachine, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			vsphereMachine.GroupVersionKind(),
			vsphereMachine.Namespace,
			vsphereMachine.Name)
	}

	// Create the machine context for this request.
	machineContext := &context.VIMMachineContext{
		BaseMachineContext: &context.BaseMachineContext{
			ControllerContext: r.ControllerContext,
			Cluster:           cluster,
			Machine:           machine,
			Logger:            r.Logger.WithName(req.Namespace).WithName(req.Name),
		},
		PatchHelper:    patchHelper,
		VSphereCluster: vsphereCluster,
		VSphereMachine: vsphereMachine,
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(machineContext.VSphereMachine,
			conditions.WithConditions(
				infrav1.VMProvisionedCondition,
			),
		)

		// Patch the VSphereMachine resource.
		if err := machineContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}
			machineContext.Logger.Error(err, "patch failed", "machine", machineContext.String())
		}
	}()

	// Handle deleted machines
	if !vsphereMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(machineContext)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(machineContext)
}

func (r machineReconciler) reconcileDelete(ctx context.MachineContext) (reconcile.Result, error) {
	ctx.GetLogger().Info("Handling deleted VSphereMachine")
	conditions.MarkFalse(ctx.GetVSphereMachine(), infrav1.VMProvisionedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")

	if err := r.VMService.ReconcileDelete(ctx); err != nil {
		if apierrors.IsNotFound(err) {
			// The VM is deleted so remove the finalizer.
			ctrlutil.RemoveFinalizer(ctx.GetVSphereMachine(), infrav1.MachineFinalizer)
			return reconcile.Result{}, nil
		}
		conditions.MarkFalse(ctx.GetVSphereMachine(), infrav1.VMProvisionedCondition, clusterv1.DeletionFailedReason, clusterv1.ConditionSeverityWarning, "")
		return reconcile.Result{}, err
	}

	// VM is being deleted
	return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r machineReconciler) reconcileNormal(ctx context.MachineContext) (reconcile.Result, error) {
	machineFailed, err := r.VMService.SyncFailureReason(ctx)
	if err != nil && !apierrors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	// If the VSphereMachine is in an error state, return early.
	if machineFailed {
		ctx.GetLogger().Info("Error state detected, skipping reconciliation")
		return reconcile.Result{}, nil
	}

	// If the VSphereMachine doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.GetVSphereMachine(), infrav1.MachineFinalizer)

	// nolint:gocritic
	if r.supervisorBased {
		err := r.setVMModifiers(ctx)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// vmwarev1.VSphereCluster doesn't set Cluster.Status.Ready until the API endpoint is available.
		if !ctx.GetCluster().Status.InfrastructureReady {
			ctx.GetLogger().Info("Cluster infrastructure is not ready yet")
			conditions.MarkFalse(ctx.GetVSphereMachine(), infrav1.VMProvisionedCondition, infrav1.WaitingForClusterInfrastructureReason, clusterv1.ConditionSeverityInfo, "")
			return reconcile.Result{}, nil
		}
	}

	// Make sure bootstrap data is available and populated.
	if ctx.GetMachine().Spec.Bootstrap.DataSecretName == nil {
		if !infrautilv1.IsControlPlaneMachine(ctx.GetVSphereMachine()) && !conditions.IsTrue(ctx.GetCluster(), clusterv1.ControlPlaneInitializedCondition) {
			ctx.GetLogger().Info("Waiting for the control plane to be initialized")
			conditions.MarkFalse(ctx.GetVSphereMachine(), infrav1.VMProvisionedCondition, clusterv1.WaitingForControlPlaneAvailableReason, clusterv1.ConditionSeverityInfo, "")
			return ctrl.Result{}, nil
		}
		ctx.GetLogger().Info("Waiting for bootstrap data to be available")
		conditions.MarkFalse(ctx.GetVSphereMachine(), infrav1.VMProvisionedCondition, infrav1.WaitingForBootstrapDataReason, clusterv1.ConditionSeverityInfo, "")
		return reconcile.Result{}, nil
	}

	requeue, err := r.VMService.ReconcileNormal(ctx)
	if err != nil {
		return reconcile.Result{}, err
	} else if requeue {
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	conditions.MarkTrue(ctx.GetVSphereMachine(), infrav1.VMProvisionedCondition)
	return reconcile.Result{}, nil
}

func (r *machineReconciler) clusterToVSphereMachines(a client.Object) []reconcile.Request {
	requests := []reconcile.Request{}
	machines, err := infrautilv1.GetMachinesInCluster(goctx.Background(), r.Client, a.GetNamespace(), a.GetName())
	if err != nil {
		return requests
	}
	for _, m := range machines {
		r := reconcile.Request{
			NamespacedName: apitypes.NamespacedName{
				Name:      m.Name,
				Namespace: m.Namespace,
			},
		}
		requests = append(requests, r)
	}
	return requests
}

func (r *machineReconciler) fetchCAPICluster(machine *clusterv1.Machine, vsphereMachine metav1.Object) *clusterv1.Cluster {
	cluster, err := clusterutilv1.GetClusterFromMetadata(r, r.Client, machine.ObjectMeta)
	if err != nil {
		r.Logger.Info("Machine is missing cluster label or cluster does not exist")
		return nil
	}
	if annotations.IsPaused(cluster, vsphereMachine) {
		r.Logger.V(4).Info("VSphereMachine %s/%s linked to a cluster that is paused", vsphereMachine.GetNamespace(), vsphereMachine.GetName())
		return nil
	}

	return cluster
}

// Return hooks that will be invoked when a VirtualMachine is created
func (r *machineReconciler) setVMModifiers(c context.MachineContext) error {
	ctx, ok := c.(*vmware.SupervisorMachineContext)
	if !ok {
		return errors.New("received unexpected MachineContext. expecting SupervisorMachineContext type")
	}

	networkModifier := func(obj runtime.Object) (runtime.Object, error) {
		// No need to check the type. We know this will be a VirtualMachine
		vm, _ := obj.(*vmoprv1.VirtualMachine)
		ctx.Logger.V(3).Info("Applying network config to VM", "vm-name", vm.Name)
		err := r.networkProvider.ConfigureVirtualMachine(ctx.ClusterContext, vm)
		if err != nil {
			return nil, errors.Errorf("failed to configure machine network: %+v", err)
		}
		return vm, nil
	}
	ctx.VMModifiers = []vmware.VMModifier{networkModifier}
	return nil
}
