/*
Copyright 2021 The Kubernetes Authors.

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

package integration

import (
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/manager"
	infrautilv1 "sigs.k8s.io/cluster-api-provider-vsphere/pkg/util"
)

// The purpose of this test is to start up a CAPI controller against a real API
// server and run Cluster tests
var _ = Describe("Cluster lifecycle tests", func() {

	var (
		mgr               *TestManager
		mgrOpts           *manager.Options
		propagationPolicy *metav1.DeletionPropagation
		mf                *Manifests
		controlPlane      *ControlPlaneComponents
		worker            *WorkerComponents
	)

	BeforeEach(func() {
		mgrOpts = &manager.Options{}
	})

	JustBeforeEach(func() {
		// Start the controller.
		mgrOpts.PodName = fmt.Sprintf("cluster-crud-controller-%s", uuid.New())
		mgr = startControllerManager(*mgrOpts)
		testNamespace := mgr.GetControllerManagerNamespace()

		By("Creating a dummy VM Image")
		dummyVMImage := generateVirtualMachineImage()
		createNonNamespacedResource(mgr, virtualmachineimageResource, dummyVMImage)

		By("Generating manifests")
		mf = generateManifests(testNamespace)

		// Only the first control plane machine is used since any additional
		// ones require an initialized control plane, something which is no
		// longer possible to simulate in CAPI v1a2 without a working API
		// endpoint.
		// Expect(mf.ControlPlaneComponentsList).Should(HaveLen(1), "control plane must have exactly one machine")
		controlPlane = mf.ControlPlaneComponentsList[0]
		worker = mf.WorkerComponents
	})

	AfterEach(func() {
		stopControllerManager(mgr)
		mgr = nil
		mgrOpts = nil
		propagationPolicy = nil
		mf = nil
		controlPlane = nil
		worker = nil
	})

	Context("Create a cluster", func() {
		JustBeforeEach(func() {
			// CREATE the CAPI Cluster and VSphereCluster resources.
			createResource(mgr, clustersResource, mf.ClusterComponents.Cluster)
			createResource(mgr, vsphereclustersResource, mf.ClusterComponents.VSphereCluster)

			// ASSERT the CAPI Cluster and the VSphereCluster resources eventually exist
			// and that the VSphereCluster has an OwnerRef that points to the CAPI
			// Cluster.
			cluster := assertEventuallyExists(mgr, clustersResource, mf.ClusterComponents.Cluster.Name, nil)
			clusterOwnerRef := toOwnerRef(cluster)
			clusterOwnerRef.Controller = pointer.BoolPtr(true)
			clusterOwnerRef.BlockOwnerDeletion = pointer.BoolPtr(true)
			assertEventuallyExists(mgr, vsphereclustersResource, mf.ClusterComponents.Cluster.Name, clusterOwnerRef)
		})

		JustAfterEach(func() {
			// DELETE the CAPI Cluster.
			deleteResource(mgr, clustersResource, mf.ClusterComponents.Cluster.Name, propagationPolicy)

			// ASSERT the CAPI Cluster and VSphereCluster are eventually deleted.
			assertEventuallyDoesNotExist(mgr, vsphereclustersResource, mf.ClusterComponents.Cluster.Name)
			assertEventuallyDoesNotExist(mgr, clustersResource, mf.ClusterComponents.Cluster.Name)
		})

		Context("with no machines", func() {
			BeforeEach(func() {
				testClusterName = "cc-testcluster1"
			})
			It("should delete successfully with default policy", func() {
				// Handled by JustAfterEach
			})
		})

		Context("with machines", func() {
			JustBeforeEach(func() {
				// CREATE the CAPI Machine, VSphereMachine, and KubeadmConfig resources for
				// the control plane machine.
				createResource(mgr, machinesResource, controlPlane.Machine)
				createResource(mgr, vspheremachinesResource, controlPlane.VSphereMachine)
				createResource(mgr, kubeadmconfigResources, controlPlane.KubeadmConfig)

				// CREATE the CAPI MachineDeplopyment, VSphereMachineTemplate,
				// and KubeadmConfigTemplate resources for the worker nodes.
				createResource(mgr, machinedeploymentResource, worker.MachineDeployment)
				createResource(mgr, vspheremachinetemplateResource, worker.VSphereMachineTemplate)
				createResource(mgr, kubeadmconfigtemplateResource, worker.KubeadmConfigTemplate)

				// ASSERT the CAPI Machine, VSphereMachine, KubeadmConfig, and VM
				// Operator VirtualMachine, and bootstrap data ConfigMap
				// resources for the control plane machine eventually exist, the
				// VSphereMachine and KubeadmConfig resources have OwnerRefs that
				// point to the CAPI Machine, and the ConfigMap resource has a
				// controller OwnerRef that points to the VSphereMachine.
				machine := assertEventuallyExists(mgr, machinesResource, controlPlane.Machine.Name, nil)
				machineOwnerRef := toOwnerRef(machine)
				machineOwnerRef.Controller = pointer.BoolPtr(true)
				machineOwnerRef.BlockOwnerDeletion = pointer.BoolPtr(true)
				assertEventuallyExists(mgr, kubeadmconfigResources, controlPlane.Machine.Name, machineOwnerRef)

				vsphereMachine := assertEventuallyExists(mgr, vspheremachinesResource, controlPlane.Machine.Name, machineOwnerRef)
				vsphereMachineOwnerRef := toControllerOwnerRef(vsphereMachine)

				assertEventuallyExists(mgr, virtualmachinesResource, controlPlane.Machine.Name, nil)
				assertEventuallyExists(mgr, configmapsResource, infrautilv1.GetBootstrapConfigMapName(controlPlane.Machine.Name), vsphereMachineOwnerRef)

				assertEventuallyExists(mgr, machinedeploymentResource, worker.MachineDeployment.Name, nil)
				assertEventuallyExists(mgr, vspheremachinetemplateResource, worker.VSphereMachineTemplate.Name, nil)
				assertEventuallyExists(mgr, kubeadmconfigtemplateResource, worker.KubeadmConfigTemplate.Name, nil)
			})
			AfterEach(func() {
				// ASSERT the CAPI Machine, VSphereMachine, KubeadmConfig, VM
				// Operator VirtualMachine, and bootstrap data ConfigMap
				// resources for the control plane machine are eventually
				// deleted.
				assertEventuallyDoesNotExist(mgr, configmapsResource, infrautilv1.GetBootstrapConfigMapName(controlPlane.Machine.Name))
				assertEventuallyDoesNotExist(mgr, virtualmachinesResource, controlPlane.Machine.Name)
				assertEventuallyDoesNotExist(mgr, vspheremachinesResource, controlPlane.Machine.Name)
				assertEventuallyDoesNotExist(mgr, kubeadmconfigResources, controlPlane.Machine.Name)
				assertEventuallyDoesNotExist(mgr, machinesResource, controlPlane.Machine.Name)

				// Assert that MachineDeployment and its descendents are eventually deleted
				assertEventuallyDoesNotExist(mgr, machinedeploymentResource, worker.MachineDeployment.Name)
				assertEventuallyDoesNotExist(mgr, vspheremachinetemplateResource, worker.VSphereMachineTemplate.Name)
				assertEventuallyDoesNotExist(mgr, kubeadmconfigtemplateResource, worker.KubeadmConfigTemplate.Name)
			})
			Context("that are not explicitly deleted", func() {
				BeforeEach(func() {
					testClusterName = "cc-testcluster2"
				})
				It("should delete the cluster, deleting the machines via propagation with default policy", func() {
					// Handled by JustBeforeEach and AfterEach
				})
				It("cluster should have a ControlPlaneEndpoint when a ControlPlane machine gets an IP", func() {
					ipAddress := "127.0.0.1"
					setIPAddressOnMachine(mgr, controlPlane.Machine.Name, ipAddress)
					assertClusterEventuallyGetsControlPlaneEndpoint(mgr, testClusterName, ipAddress)
				})
			})
			Context("that are explicitly deleted before the cluster", func() {
				BeforeEach(func() {
					testClusterName = "cc-testcluster3"
				})
				It("should delete both the machines and cluster successfully when Machine is deleted", func() {
					// DELETE the CAPI Machine, VSphereMachine, and KubeadmConfig resources for
					// the control plane machine.
					// These are all deleted as a side effect of deleting the Machine due to ownerReferences
					deleteResource(mgr, machinesResource, controlPlane.Machine.Name, nil)
				})
				It("should delete both the machines and cluster successfully when VSphereMachine is deleted", func() {
					// DELETE the VSphereMachine resource for the control plane machine
					// Expect the cluster and everything else to be cleaned up by JustAfterEach
					deleteResource(mgr, vspheremachinesResource, controlPlane.Machine.Name, nil)
				})
			})
		})
	})
})
