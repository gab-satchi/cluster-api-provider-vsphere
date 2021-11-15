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

//nolint
package integration

import (
	ctx "context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/docker/distribution/context"
	goctx "golang.org/x/net/context"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructuredv1 "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	vmoprv1 "github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	infrav1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/manager"
)

const (
	loglevel                           = "5"
	waitTimeSecsForExists              = 30
	dummyVirtualMachineImageName       = "dummy-image"
	dummyDistributionVersion           = "dummy-distro.123"
	dummyImageRepository               = "vmware"
	dummyDnsVersion                    = "v1.3.1_vmware.1"
	dummyEtcdVersion                   = "v3.3.10_vmware.1"
	numControlPlaneMachines            = 1
	controlPlaneMachineClassName       = "dummy-control-plane-class"
	controlPlaneMachineStorageClass    = "dummy-control-plane-storage-class"
	controlPlaneEndPoint               = "https://dummy-lb:6443"
	numWorkerMachines                  = 1
	VirtualMachineDistributionProperty = "vmware-system.guest.kubernetes.distribution.image.version"
)

var (
	testClusterName        string
	dummyKubernetesVersion = "1.15.0+vmware.1"
)

var (
	intervals = []interface{}{
		time.Second * time.Duration(waitTimeSecsForExists),
		time.Second * 1,
	}

	clustersResource = schema.GroupVersionResource{
		Group:    clusterv1.GroupVersion.Group,
		Version:  clusterv1.GroupVersion.Version,
		Resource: "clusters",
	}

	vsphereclustersResource = schema.GroupVersionResource{
		Group:    infrav1.GroupVersion.Group,
		Version:  infrav1.GroupVersion.Version,
		Resource: "vsphereclusters",
	}

	machinesResource = schema.GroupVersionResource{
		Group:    clusterv1.GroupVersion.Group,
		Version:  clusterv1.GroupVersion.Version,
		Resource: "machines",
	}

	machinedeploymentResource = schema.GroupVersionResource{
		Group:    clusterv1.GroupVersion.Group,
		Version:  clusterv1.GroupVersion.Version,
		Resource: "machinedeployments",
	}

	vspheremachinesResource = schema.GroupVersionResource{
		Group:    infrav1.GroupVersion.Group,
		Version:  infrav1.GroupVersion.Version,
		Resource: "vspheremachines",
	}

	vspheremachinetemplateResource = schema.GroupVersionResource{
		Group:    infrav1.GroupVersion.Group,
		Version:  infrav1.GroupVersion.Version,
		Resource: "vspheremachinetemplates",
	}

	kubeadmconfigResources = schema.GroupVersionResource{
		Group:    bootstrapv1.GroupVersion.Group,
		Version:  bootstrapv1.GroupVersion.Version,
		Resource: "kubeadmconfigs",
	}

	kubeadmconfigtemplateResource = schema.GroupVersionResource{
		Group:    bootstrapv1.GroupVersion.Group,
		Version:  bootstrapv1.GroupVersion.Version,
		Resource: "kubeadmconfigtemplates",
	}

	namespacesResource = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}

	configmapsResource = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}

	eventsResource = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "events",
	}

	virtualmachinesResource = schema.GroupVersionResource{
		Group:    vmoprv1.SchemeGroupVersion.Group,
		Version:  vmoprv1.SchemeGroupVersion.Version,
		Resource: "virtualmachines",
	}

	virtualmachineimageResource = schema.GroupVersionResource{
		Group:    vmoprv1.SchemeGroupVersion.Group,
		Version:  vmoprv1.SchemeGroupVersion.Version,
		Resource: "virtualmachineimages",
	}
)

// TestManager wraps InitializedManager with other test-related state
type TestManager struct {
	manager.Manager
	client dynamic.Interface
	done   chan struct{}

	cancelFunc goctx.CancelFunc
}

// Manifests contains the resources required to deploy a cluster with CAPV.
type Manifests struct {
	ClusterComponents *ClusterComponents

	ControlPlaneComponentsList []*ControlPlaneComponents

	WorkerComponents *WorkerComponents
}

type ClusterComponents struct {
	Cluster        *clusterv1.Cluster
	VSphereCluster *infrav1.VSphereCluster
}

// ControlPlaneComponents contains the resources required to create a control
// plane machine.
type ControlPlaneComponents struct {
	Machine        *clusterv1.Machine
	VSphereMachine *infrav1.VSphereMachine
	KubeadmConfig  *bootstrapv1.KubeadmConfig
}

// WorkerComponents contains the resources required to create a
// MachineDeployment.
type WorkerComponents struct {
	MachineDeployment      *clusterv1.MachineDeployment
	VSphereMachineTemplate *infrav1.VSphereMachineTemplate
	KubeadmConfigTemplate  *bootstrapv1.KubeadmConfigTemplate
}

func TestCAPV(t *testing.T) {
	BeforeSuite(func() {
		// Set log level
		flags := flag.NewFlagSet("flags", flag.PanicOnError)
		klog.InitFlags(flags)
		ctrl.SetLogger(klogr.New())
		err := flags.Parse([]string{"-v", loglevel})
		Expect(err).NotTo(HaveOccurred())
	})
	RegisterFailHandler(Fail)
	RunSpecs(t, "CAPV integration tests")
}

// The test will run against a local kubernetes cluster
// Prerequisites are that all CAPV dependencies and vm-operator CRDs are loaded
func getTestEnv() *rest.Config {
	config, err := config.GetConfig()
	Expect(err).NotTo(HaveOccurred())
	return config
}

// Create a dynamic client
func getTestClient(opts manager.Options) dynamic.Interface {
	if opts.KubeConfig == nil {
		opts.KubeConfig = getTestEnv()
	}

	return dynamic.NewForConfigOrDie(opts.KubeConfig)
}

// Blocks until Manager has initialized
func startControllerManager(opts manager.Options) *TestManager {
	client := getTestClient(opts)

	// Ensure each manager has a unique metrics addr so there are no port
	// conflicts.
	opts.MetricsAddr = fmt.Sprintf("127.0.0.1:%d", randomTCPPort())

	// Create the namespace in which the controller should run.
	createTestNamespace(client, &opts)

	// Create a new CAPV controller manager.
	mgr, err := manager.New(opts)
	Expect(err).NotTo(HaveOccurred())

	// Start the CAPV controller manager.
	done := make(chan struct{})
	var mgrCancelFunc goctx.CancelFunc
	go func() {
		klog.Info("Starting the manager")
		// TODO: Aarti change
		//if err := mgr.Start(done); err != nil {
		mgrCtx, mgrCancelFunc := goctx.WithCancel(mgr.GetContext())
		err := mgr.Start(mgrCtx)
		if err != nil {
			klog.Fatal(err, "unable to run the manager")
			mgrCancelFunc()
		}
	}()

	// Wait for the cache to sync, indicating the manager is initialized.
	Expect(waitForCacheToSync(mgr, done)).ShouldNot(HaveOccurred(), "Cache should have sync'd")

	return &TestManager{
		Manager: mgr,
		client:  client,
		cancelFunc: mgrCancelFunc,
	}
}

func stopControllerManager(manager *TestManager) {
	// Shutdown the manager.
	manager.cancelFunc()

	// Delete the test namespace.
	err := manager.client.Resource(namespacesResource).Delete(manager.GetContext(), manager.GetControllerManagerNamespace(), metav1.DeleteOptions{})
	Expect(err).ShouldNot(HaveOccurred())
}

// createTestNamespace creates the namespace in which the controller will run.
func createTestNamespace(client dynamic.Interface, opts *manager.Options) {
	if opts.PodNamespace == "" {
		opts.PodNamespace = fmt.Sprintf("capv-test-%s", uuid.New())
	}
	if opts.WatchNamespace == "" {
		opts.WatchNamespace = opts.PodNamespace
	}
	controllerNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.PodNamespace,
		},
	}
	input := toUnstructured(controllerNamespace.Name, controllerNamespace, false)
	_, err := client.Resource(namespacesResource).Create(context.Background(), input, metav1.CreateOptions{})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			Expect(err).ShouldNot(HaveOccurred())
		}
	}
}

// waitForCacheToSync waits for the manager to have initialized.
// There is no way to actually query the controller manager to determine
// whether it has started.
func waitForCacheToSync(mgr ctrl.Manager, done chan struct{}) error {
	count := 0
	for {
		select {
		case <-done:
			return errors.New("manager stopped while waiting for cache to sync")
		default:
			time.Sleep(time.Second * 1)
		}
		if mgr.GetCache().WaitForCacheSync(context.Background()) {
			return nil
		}
		if count == 15 {
			return errors.New("cached should have sync'd")
		}
		count++
	}
}

type ImageVersion struct {
	ImageRepository string `json:"imageRepository"`
	Version         string `json:"version"`
}

type VirtualMachineDistributionSpec struct {
	Version    string       `json:"version"`
	Kubernetes ImageVersion `json:"kubernetes"`
	Etcd       ImageVersion `json:"etcd"`
	CoreDNS    ImageVersion `json:"coredns"`
}

func generateVirtualMachineImage() *vmoprv1.VirtualMachineImage {
	annotations := map[string]string{}

	spec := &VirtualMachineDistributionSpec{
		Version: dummyDistributionVersion,
		Kubernetes: ImageVersion{
			ImageRepository: dummyImageRepository,
			Version:         dummyKubernetesVersion,
		},
		Etcd: ImageVersion{
			ImageRepository: dummyImageRepository,
			Version:         dummyEtcdVersion,
		},
		CoreDNS: ImageVersion{
			ImageRepository: dummyImageRepository,
			Version:         dummyDnsVersion,
		},
	}

	rawJSON, err := json.Marshal(spec)
	Expect(err).NotTo(HaveOccurred())

	annotations[VirtualMachineDistributionProperty] = string(rawJSON)

	return &vmoprv1.VirtualMachineImage{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VirtualMachineImage",
			APIVersion: virtualmachineimageResource.GroupVersion().String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        dummyVirtualMachineImageName,
			Annotations: annotations,
		},
	}
}

func generateManifests(testNamespace string) *Manifests {
	By("Creating Cluster Components")
	clusterComponents := createClusterComponents(testNamespace)

	By("Creating ControlPlane Components List")
	controlPlaneComponentsList := createControlPlaneComponentsList(testNamespace)

	By("Creating Worker Components")
	workerComponents := createWorkerComponents(testNamespace)

	return &Manifests{
		ClusterComponents: clusterComponents,

		ControlPlaneComponentsList: controlPlaneComponentsList,

		WorkerComponents: workerComponents,
	}
}

func createClusterComponents(testNamespace string) *ClusterComponents {
	By("Creating a VSphereCluster")
	vsphereCluster := infrav1.VSphereCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       "VSphereCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName,
			Namespace: testNamespace,
		},
	}

	By("Creating a Cluster")
	cluster := clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName,
			Namespace: testNamespace,
		},
		Spec: clusterv1.ClusterSpec{
			ClusterNetwork: &clusterv1.ClusterNetwork{
				Services: &clusterv1.NetworkRanges{
					CIDRBlocks: []string{"100.64.0.0/13"},
				},
				Pods: &clusterv1.NetworkRanges{
					CIDRBlocks: []string{"100.96.0.0/11"},
				},
				ServiceDomain: "cluster.local",
			},
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       "VSphereCluster",
				Name:       vsphereCluster.Name,
				Namespace:  vsphereCluster.Namespace,
			},
		},
	}

	return &ClusterComponents{
		Cluster:        &cluster,
		VSphereCluster: &vsphereCluster,
	}
}

func createControlPlaneComponentsList(testNamespace string) []*ControlPlaneComponents {

	cpMachineNameFmt := "%s-control-plane-%d"
	var controlPlaneComponentsList []*ControlPlaneComponents

	for i := 0; i < numControlPlaneMachines; i++ {
		var controlPlaneComponents ControlPlaneComponents

		By("Creating a VSphereMachine")
		vsphereMachine := infrav1.VSphereMachine{
			TypeMeta: metav1.TypeMeta{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       "VSphereMachine",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(cpMachineNameFmt, testClusterName, i),
				Namespace: testNamespace,
				Labels: map[string]string{
					clusterv1.MachineControlPlaneLabelName: "true",
					clusterv1.ClusterLabelName:             testClusterName,
				},
			},
			Spec: infrav1.VSphereMachineSpec{
				ImageName:    dummyVirtualMachineImageName,
				ClassName:    controlPlaneMachineClassName,
				StorageClass: controlPlaneMachineStorageClass,
			},
		}
		controlPlaneComponents.VSphereMachine = &vsphereMachine

		By("Creating KubeadmConfigs")

		kubeadmConfig := bootstrapv1.KubeadmConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: bootstrapv1.GroupVersion.String(),
				Kind:       "KubeadmConfig",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(cpMachineNameFmt, testClusterName, i),
				Namespace: testNamespace,
			},
			Spec: bootstrapv1.KubeadmConfigSpec{
				ClusterConfiguration: &bootstrapv1.ClusterConfiguration{
					ClusterName: testClusterName,
				},
				InitConfiguration: &bootstrapv1.InitConfiguration{},
				JoinConfiguration: &bootstrapv1.JoinConfiguration{},
			},
		}
		controlPlaneComponents.KubeadmConfig = &kubeadmConfig

		By("Creating Machines")
		machine := clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(cpMachineNameFmt, testClusterName, i),
				Namespace: testNamespace,
				Labels: map[string]string{
					clusterv1.MachineControlPlaneLabelName: "true",
					clusterv1.ClusterLabelName:             testClusterName,
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: testClusterName,
				Version:     &dummyKubernetesVersion,
				Bootstrap: clusterv1.Bootstrap{
					ConfigRef: &corev1.ObjectReference{
						APIVersion: bootstrapv1.GroupVersion.String(),
						Kind:       "KubeadmConfig",
						Name:       kubeadmConfig.Name,
						Namespace:  kubeadmConfig.Namespace,
					},
				},
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: infrav1.GroupVersion.String(),
					Kind:       "VSphereMachine",
					Name:       vsphereMachine.Name,
					Namespace:  vsphereMachine.Namespace,
				},
			},
		}

		controlPlaneComponents.Machine = &machine

		// Collect controlPlaneComponents
		controlPlaneComponentsList = append(controlPlaneComponentsList, &controlPlaneComponents)
	}

	return controlPlaneComponentsList
}

func createWorkerComponents(testNamespace string) *WorkerComponents {
	workerMachineDeploymentNameFmt := "%s-workers-0"

	By("Creating VSphereMachineTemplates")
	vsphereMachineTemplate := infrav1.VSphereMachineTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       "VSphereMachineTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(workerMachineDeploymentNameFmt, testClusterName),
			Namespace: testNamespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: testClusterName,
			},
		},
	}

	By("Creating a KubeadmConfigTemplate")
	kubeadmConfigTemplate := bootstrapv1.KubeadmConfigTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: bootstrapv1.GroupVersion.String(),
			Kind:       "KubeadmConfigTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(workerMachineDeploymentNameFmt, testClusterName),
			Namespace: testNamespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: testClusterName,
			},
		},
	}

	By("Creating a MachineDeployment")
	numWorker := int32(numWorkerMachines)
	machineDeployment := clusterv1.MachineDeployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "MachineDeployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(workerMachineDeploymentNameFmt, testClusterName),
			Namespace: testNamespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: testClusterName,
			},
		},
		Spec: clusterv1.MachineDeploymentSpec{
			ClusterName: testClusterName,
			Replicas:    &numWorker,
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					clusterv1.ClusterLabelName: testClusterName,
				},
			},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterLabelName: testClusterName,
					},
				},
				Spec: clusterv1.MachineSpec{
					ClusterName: testClusterName,
					Version:     &dummyKubernetesVersion,
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: bootstrapv1.GroupVersion.String(),
							Kind:       "KubeadmConfigTemplate",
							Name:       kubeadmConfigTemplate.Name,
							Namespace:  kubeadmConfigTemplate.Namespace,
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: infrav1.GroupVersion.String(),
						Kind:       "VSphereMachineTemplate",
						Name:       vsphereMachineTemplate.Name,
						Namespace:  vsphereMachineTemplate.Namespace,
					},
				},
			},
		},
	}

	return &WorkerComponents{
		MachineDeployment:      &machineDeployment,
		VSphereMachineTemplate: &vsphereMachineTemplate,
		KubeadmConfigTemplate:  &kubeadmConfigTemplate,
	}
}

func createNonNamespacedResource(manager *TestManager, resource schema.GroupVersionResource, obj runtimeObjectWithName) {
	input := toUnstructured(obj.GetName(), obj, false)
	_, err := manager.client.Resource(resource).Create(manager.GetContext(), input, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred(), "Error creating %s %s", resource, obj.GetName())
}

func createResource(manager *TestManager, resource schema.GroupVersionResource, obj runtimeObjectWithName) {
	input := toUnstructured(obj.GetName(), obj, false)
	_, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Create(manager.GetContext(), input, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "Error creating %s %s/%s", resource, obj.GetNamespace(), obj.GetName())
}

func deleteResource(manager *TestManager, resource schema.GroupVersionResource, name string, propagationPolicy *metav1.DeletionPropagation) {
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: propagationPolicy}
	err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Delete(manager.GetContext(), name, deleteOptions)
	Expect(err).NotTo(HaveOccurred(), "Error deleting %s %s", resource, name)
}

func getResource(manager *TestManager, resource schema.GroupVersionResource, name string, obj runtime.Object) {
	output, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Get(manager.GetContext(), name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "Error getting %s %s", resource, name)
	toStructured(name, obj, output)
}

// Note that this does not update the Status of an object. See updateResourceStatus()
func updateResource(manager *TestManager, resource schema.GroupVersionResource, obj runtimeObjectWithName) {
	input := toUnstructured(obj.GetName(), obj, false)
	_, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Update(manager.GetContext(), input, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "Error updating %s %s/%s", resource, obj.GetNamespace(), obj.GetName())
}

func updateResourceStatus(manager *TestManager, resource schema.GroupVersionResource, obj runtimeObjectWithName) {
	input := toUnstructured(obj.GetName(), obj, true)
	_, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).UpdateStatus(manager.GetContext(), input, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "Error updating status of %s %s/%s", resource, obj.GetNamespace(), obj.GetName())
}

func assertEventuallyExists(manager *TestManager, resource schema.GroupVersionResource, name string, ownerRef *metav1.OwnerReference) *unstructuredv1.Unstructured {
	var obj *unstructuredv1.Unstructured
	EventuallyWithOffset(1, func() (bool, error) {
		output, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Get(manager.GetContext(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if ownerRef != nil {
			foundOwnerRef := false
			for _, ref := range output.GetOwnerReferences() {
				if ref.APIVersion != ownerRef.APIVersion {
					continue
				}
				if ref.Kind != ownerRef.Kind {
					continue
				}
				if ref.Name != ownerRef.Name {
					continue
				}
				if ref.UID != ownerRef.UID {
					continue
				}
				if ref.Controller != nil || ownerRef.Controller != nil {
					if ref.Controller == nil && ownerRef.Controller != nil {
						continue
					} else if ref.Controller != nil && ownerRef.Controller == nil {
						continue
					} else if *ref.Controller != *ownerRef.Controller {
						continue
					}
				}
				if ref.BlockOwnerDeletion != nil || ownerRef.BlockOwnerDeletion != nil {
					if ref.BlockOwnerDeletion == nil && ownerRef.BlockOwnerDeletion != nil {
						continue
					} else if ref.BlockOwnerDeletion != nil && ownerRef.BlockOwnerDeletion == nil {
						continue
					} else if *ref.BlockOwnerDeletion != *ownerRef.BlockOwnerDeletion {
						continue
					}
				}
				foundOwnerRef = true
				break
			}
			if !foundOwnerRef {
				return false, errors.Errorf(
					"Unable to find expected OwnerRef %+v for %s %s/%s: %+v",
					*ownerRef,
					output.GroupVersionKind(),
					output.GetNamespace(),
					output.GetName(),
					output.GetOwnerReferences())
			}
		}
		obj = output
		return true, nil
	}, intervals...).Should(BeTrue(), "should exist %s %s", resource, name)
	return obj
}

func assertEventuallyDoesNotExist(manager *TestManager, resource schema.GroupVersionResource, name string) {
	EventuallyWithOffset(1, func() (bool, error) {
		_, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Get(manager.GetContext(), name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, intervals...).Should(BeTrue(), "should not exist %s %s", resource, name)
}

func assertConsistentlyDoesNotExist(manager *TestManager, resource schema.GroupVersionResource, name string) {
	ConsistentlyWithOffset(1, func() (bool, error) {
		_, err := manager.client.Resource(resource).Namespace(manager.GetControllerManagerNamespace()).Get(ctx.Background(), name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, intervals...).Should(BeTrue(), "should not exist %s %s", resource, name)
}

func assertEventuallyEvents(manager *TestManager, obj runtimeObjectWithName, reason string) {
	EventuallyWithOffset(1, func() (bool, error) {
		listOptions := metav1.ListOptions{Limit: 200}
		output, err := manager.client.Resource(eventsResource).Namespace(manager.GetControllerManagerNamespace()).List(ctx.Background(), listOptions)
		if err != nil {
			return false, nil
		}
		if !output.IsList() {
			return false, errors.New("empty events result")
		}
		for _, outputItem := range output.Items {
			event := &corev1.Event{}

			toStructured(outputItem.GetName(), event, &outputItem)
			if event.Reason != reason {
				continue
			}
			if event.InvolvedObject.APIVersion != obj.GroupVersionKind().GroupVersion().String() {
				continue
			}
			if event.InvolvedObject.Kind != obj.GroupVersionKind().Kind {
				continue
			}
			if event.InvolvedObject.Name != obj.GetName() {
				continue
			}
			if event.InvolvedObject.UID != obj.GetUID() {
				continue
			}
			return true, nil
		}
		return false, errors.New("no matching events")
	}, intervals...).Should(BeTrue(), "event %s did not occur for %s %s", reason, obj.GroupVersionKind(), obj.GetName())
}

func assertVirtualMachineState(manager *TestManager, machine *clusterv1.Machine, vm *vmoprv1.VirtualMachine) {
	Expect(vm.Name).Should(Equal(machine.Name))
	Expect(vm.Namespace).Should(Equal(manager.GetControllerManagerNamespace()))
	Expect(vm.Spec.ImageName).ShouldNot(BeEmpty())
	Expect(machine.Spec.Version).ShouldNot(BeNil(), "Error accessing nil Spec.Version for machine %s", machine.Name)
	Expect(vm.Spec.VmMetadata).NotTo(BeNil())
	// TODO: Aarti: not sure where to get this varaible from
	//Expect(vm.Spec.VmMetadata.Transport).To(Equal(vmoprv1.VirtualMachineMetadataExtraConfigTransport))
	Expect(vm.Spec.VmMetadata.ConfigMapName).ToNot(BeNil())
}

// assertClusterEventuallyGetsControlPlaneEndpoint ensures that the cluster
// receives a control plane endpoint that matches the expected IP address
func assertClusterEventuallyGetsControlPlaneEndpoint(manager *TestManager, clusterName string, ipAddress string) {
	EventuallyWithOffset(1, func() bool {
		vsphereCluster := &infrav1.VSphereCluster{}
		getResource(manager, vsphereclustersResource, clusterName, vsphereCluster)
		// If the control plane endpoint is undefined, return false
		if vsphereCluster.Spec.ControlPlaneEndpoint.IsZero() {
			return false
		}

		return vsphereCluster.Spec.ControlPlaneEndpoint.Host == ipAddress
	}, intervals...).Should(BeTrue(), "Expected ControlPlaneEndpoint was not set")
}

func toUnstructured(name string, obj runtime.Object, preserveStatus bool) *unstructuredv1.Unstructured {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if !preserveStatus {
		delete(data, "status")
	}
	Expect(err).NotTo(
		HaveOccurred(),
		"Error getting unstructured data for %s: %q",
		obj.GetObjectKind().GroupVersionKind(), name)
	return &unstructuredv1.Unstructured{Object: data}
}

func toStructured(name string, dst runtime.Object, src *unstructuredv1.Unstructured) {
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(src.UnstructuredContent(), dst)
	Expect(err).NotTo(
		HaveOccurred(),
		"Error getting structured object for %s: %q",
		src.GroupVersionKind(), name)
}

type canBeReferenced interface {
	GetUID() types.UID
	GetName() string
	GroupVersionKind() schema.GroupVersionKind
}

type runtimeObjectWithName interface {
	canBeReferenced
	runtime.Object
	GetNamespace() string
}

func toOwnerRef(obj canBeReferenced) *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion: obj.GroupVersionKind().GroupVersion().String(),
		Kind:       obj.GroupVersionKind().Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
}

func toControllerOwnerRef(obj canBeReferenced) *metav1.OwnerReference {
	ptrBool := true
	return &metav1.OwnerReference{
		APIVersion:         obj.GroupVersionKind().GroupVersion().String(),
		Kind:               obj.GroupVersionKind().Kind,
		Name:               obj.GetName(),
		UID:                obj.GetUID(),
		Controller:         &ptrBool,
		BlockOwnerDeletion: &ptrBool,
	}
}

func setIPAddressOnMachine(manager *TestManager, machineName, ipAddress string) {
	vsphereMachine := &infrav1.VSphereMachine{}
	getResource(manager, vspheremachinesResource, machineName, vsphereMachine)
	vsphereMachine.Status.IPAddr = ipAddress
	updateResourceStatus(manager, vspheremachinesResource, vsphereMachine)
}

const (
	minTCPPort         = 0
	maxTCPPort         = 65535
	maxReservedTCPPort = 1024
	maxRandTCPPort     = maxTCPPort - (maxReservedTCPPort + 1)
)

var (
	tcpPortRand = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// isTCPPortAvailable returns a flag indicating whether or not a TCP port is
// available.
func isTCPPortAvailable(port int) bool {
	if port < minTCPPort || port > maxTCPPort {
		return false
	}
	conn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// randomTCPPort gets a free, random TCP port between 1025-65535. If no free
// ports are available -1 is returned.
func randomTCPPort() int {
	for i := maxReservedTCPPort; i < maxTCPPort; i++ {
		p := tcpPortRand.Intn(maxRandTCPPort) + maxReservedTCPPort + 1
		if isTCPPortAvailable(p) {
			return p
		}
	}
	return -1
}
