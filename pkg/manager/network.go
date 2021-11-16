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

package manager

import (
	"os"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/context"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/services"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/services/network"
)

const (
	NodeNetworkCMNamespaceKey = "NODE_NETWORK_CM_NAMESPACE"
	NodeNetworkCMNameKey      = "NODE_NETWORK_CM_NAME"

	NSXNetworkProviderCMKey = "NSX"
	DisableFWCMKey          = "DisableFW"
	TrueCMValue             = "true"

	VDSNetworkProviderCMKey = "vsphere-network"

	DummyLBNetworkProviderCMKey = "DummyLBNetworkProvider"
)

// NodeNetworkCM is the default value for the node network ConfigMap
var NodeNetworkCM = types.NamespacedName{
	Namespace: "capw-namespace",
	Name:      "capw-node-network-config",
}

func init() {
	NodeNetworkCM = types.NamespacedName{
		Namespace: getEnvAsStringOrFallback(NodeNetworkCMNamespaceKey, NodeNetworkCM.Namespace),
		Name:      getEnvAsStringOrFallback(NodeNetworkCMNameKey, NodeNetworkCM.Name),
	}
}

// GetNetworkProvider will return a network provider instance based on the environment
// the cfg is used to initialize a client that talks directly to api-server without using the cache
func GetNetworkProvider(ctx *context.ControllerManagerContext, cfg *rest.Config) (services.NetworkProvider, error) {
	data, err := getNodeNetworkCMData(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if data != nil {
		if value, exist := data[NSXNetworkProviderCMKey]; exist && value == TrueCMValue {
			ctx.Logger.Info("Pick NSX-T network provider")

			disableFW := data[DisableFWCMKey]
			if disableFW == TrueCMValue {
				ctx.Logger.Info("Disable FW on Guest Cluster Network")
			}

			return network.NsxtNetworkProvider(ctx.Client, disableFW), nil
		}

		if value, exist := data[VDSNetworkProviderCMKey]; exist && value == TrueCMValue {
			ctx.Logger.Info("Pick NetOp (VDS) network provider")
			return network.NetOpNetworkProvider(ctx.Client), nil
		}

		// DummyLBNetworkProvider is used for testing
		if value, exist := data[DummyLBNetworkProviderCMKey]; exist && value == TrueCMValue {
			ctx.Logger.Info("Pick Dummy LB network provider")
			return network.DummyLBNetworkProvider(), nil
		}
	}

	// If nothing is explicitly selected, pick the DummyNetworkProvider
	ctx.Logger.Info("Pick Dummy network provider")
	return network.DummyNetworkProvider(), nil
}

// getNodeNetworkCMData will attempt to find the config map in vmware-system-capv namespace and determine its data
func getNodeNetworkCMData(ctx *context.ControllerManagerContext, cfg *rest.Config) (map[string]string, error) {
	client := kubernetes.NewForConfigOrDie(cfg)
	configMap, err := client.CoreV1().ConfigMaps(NodeNetworkCM.Namespace).Get(ctx, NodeNetworkCM.Name, metav1.GetOptions{})

	if err != nil {
		ctx.Logger.Error(err, "Failed to read configmap")
		if apierrors.IsNotFound(err) {
			// TODO: This should be fatal to the manager start unless we can guarantee the CM pre-exists the manager
			return nil, nil
		}
		return nil, err
	}
	return configMap.Data, nil
}

func getEnvAsStringOrFallback(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
