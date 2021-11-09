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

package util

import (
	goctx "context"
	"fmt"

	infrav1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context/vmware"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	clusterKind      = "Cluster"
	infraClusterKind = "VSphereCluster"
)

func CreateCluster(clusterName string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       clusterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       infraClusterKind,
				Name:       clusterName,
			},
		},
	}
}

func CreateVSphereCluster(clusterName string) *infrav1.VSphereCluster {
	return &infrav1.VSphereCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       infraClusterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}
}

func createScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrav1.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	return scheme
}

func CreateClusterContext(cluster *clusterv1.Cluster, vsphereCluster *infrav1.VSphereCluster) *vmware.ClusterContext {
	scheme := createScheme()
	controllerManagerContext := &context.ControllerManagerContext{
		Context: goctx.Background(),
		Logger:  klogr.New().WithName("controller-manager-logger"),
		Scheme:  scheme,
		Client:  testclient.NewClientBuilder().WithScheme(scheme).Build(),
	}

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: controllerManagerContext,
		Logger:                   controllerManagerContext.Logger.WithName("controller-logger"),
	}

	// Build the cluster context.
	return &vmware.ClusterContext{
		ControllerContext: controllerContext,
		Logger:            controllerContext.Logger.WithName("cluster-context-logger"),
		Cluster:           cluster,
		VSphereCluster:    vsphereCluster,
	}
}

// GetBootstrapConfigMapName returns the name of the bootstrap data ConfigMap
// for a VM Operator VirtualMachine.
func GetBootstrapConfigMapName(machineName string) string {
	return fmt.Sprintf("%s-cloud-init", machineName)
}

