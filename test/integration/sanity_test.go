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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/google/uuid"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	vmoprv1 "github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"

	infrav1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	//infrav1 "gitlab.eng.vmware.com/core-build/cluster-api-provider-wcp/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/manager"
	infrautilv1 "sigs.k8s.io/cluster-api-provider-vsphere/pkg/util"
	//infrautilv1 "gitlab.eng.vmware.com/core-build/cluster-api-provider-wcp/pkg/cloud/wcp/util"
)

// The purpose of this test is to start up a CAPI controller against a real API
// server and run basic checks.
var _ = Describe("Sanity tests", func() {

	var (
		mgr          *TestManager
		mgrOpts      *manager.Options
		mf           *Manifests
		controlPlane *ControlPlaneComponents
	)

	BeforeEach(func() {
		mgrOpts = &manager.Options{}
	})

	JustBeforeEach(func() {
		// Start the controller.
		mgrOpts.PodName = fmt.Sprintf("sanity-test-controller-%s", uuid.New())
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
		Expect(mf.ControlPlaneComponentsList).Should(HaveLen(1), "control plane must have exactly one machine")
		controlPlane = mf.ControlPlaneComponentsList[0]

		// CREATE the CAPI Cluster and WCPCluster resources.
		createResource(mgr, clustersResource, mf.ClusterComponents.Cluster)
		createResource(mgr, vsphereclustersResource, mf.ClusterComponents.VSphereCluster)

		// ASSERT the CAPI Cluster and the WCPCluster resources eventually exist
		// and that the WCPCluster has an OwnerRef that points to the CAPI Cluster.
		cluster := assertEventuallyExists(mgr, clustersResource, mf.ClusterComponents.Cluster.Name, nil)
		clusterOwnerRef := toOwnerRef(cluster)
		clusterOwnerRef.Controller = pointer.BoolPtr(true)
		clusterOwnerRef.BlockOwnerDeletion = pointer.BoolPtr(true)
		assertEventuallyExists(mgr, vsphereclustersResource, mf.ClusterComponents.Cluster.Name, clusterOwnerRef)

		// CREATE the CAPI Machine, WCPMachine, and KubeadmConfig resources for
		// the control plane machine.
		createResource(mgr, machinesResource, controlPlane.Machine)
		createResource(mgr, vspheremachinesResource, controlPlane.VSphereMachine)
		createResource(mgr, kubeadmconfigResources, controlPlane.KubeadmConfig)

		// ASSERT the CAPI Machine, WCPMachine, and KubeadmConfig resources
		// for the control plane machine eventually exist and that the
		// WCPMachine and KubeadmConfig resources have OwnerRefs that point to
		// the CAPI Machine.
		machine := assertEventuallyExists(mgr, machinesResource, controlPlane.Machine.Name, nil)
		machineOwnerRef := toOwnerRef(machine)
		machineOwnerRef.Controller = pointer.BoolPtr(true)
		machineOwnerRef.BlockOwnerDeletion = pointer.BoolPtr(true)
		assertEventuallyExists(mgr, vspheremachinesResource, controlPlane.Machine.Name, machineOwnerRef)
		assertEventuallyExists(mgr, kubeadmconfigResources, controlPlane.Machine.Name, machineOwnerRef)
	})

	AfterEach(func() {
		stopControllerManager(mgr)
		mgr = nil
		mgrOpts = nil
		mf = nil
		controlPlane = nil
	})

	JustAfterEach(func() {
		// DELETE the CAPI Machine, WCPMachine, and KubeadmConfig resources for
		// the control plane machine.
		deleteResource(mgr, machinesResource, controlPlane.Machine.Name, nil)

		// ASSERT the CAPI Machine, WCPMachine, KubeadmConfig, VM
		// Operator VirtualMachine, and bootstrap data ConfigMap
		// resources for the control plane machine are eventually
		// deleted.
		assertEventuallyDoesNotExist(mgr, configmapsResource, infrautilv1.GetBootstrapConfigMapName(controlPlane.Machine.Name))
		assertEventuallyDoesNotExist(mgr, virtualmachinesResource, controlPlane.Machine.Name)
		assertEventuallyDoesNotExist(mgr, vspheremachinesResource, controlPlane.Machine.Name)
		assertEventuallyDoesNotExist(mgr, kubeadmconfigResources, controlPlane.Machine.Name)
		assertEventuallyDoesNotExist(mgr, machinesResource, controlPlane.Machine.Name)

		// DELETE the CAPI Cluster.
		deleteResource(mgr, clustersResource, mf.ClusterComponents.Cluster.Name, nil)

		// ASSERT the CAPI Cluster and WCPCLuster are eventually deleted.
		assertEventuallyDoesNotExist(mgr, vsphereclustersResource, mf.ClusterComponents.Cluster.Name)
		assertEventuallyDoesNotExist(mgr, clustersResource, mf.ClusterComponents.Cluster.Name)
	})

	Context("Happy paths", func() {
		BeforeEach(func() {
			testClusterName = "sanity-testcluster"
		})

		It("Check Basic VirtualMachine creation", func() {
			// GET the associated CAPI Machine.
			machine := &clusterv1.Machine{}
			getResource(mgr, machinesResource, controlPlane.Machine.Name, machine)

			// GET the associated VSphereMachine.
			vsphereMachine := &infrav1.VSphereMachine{}
			getResource(mgr, vspheremachinesResource, controlPlane.Machine.Name, vsphereMachine)

			// ASSERT the VirtualMachine and bootstrap data ConfigMap resources
			// eventually exist. Ensure ConfigMap has OwnerRef set to the WCPMachine.
			vmObj := assertEventuallyExists(mgr, virtualmachinesResource, controlPlane.Machine.Name, nil)
			assertEventuallyExists(mgr, configmapsResource, infrautilv1.GetBootstrapConfigMapName(controlPlane.Machine.Name), toControllerOwnerRef(vsphereMachine))
			vm := &vmoprv1.VirtualMachine{}
			toStructured(controlPlane.Machine.Name, vm, vmObj)

			// ASSERT the VirtualMachine resource has the expected state.
			assertVirtualMachineState(mgr, machine, vm)

		})
	})
	Context("Failure states", func() {
	})
})
