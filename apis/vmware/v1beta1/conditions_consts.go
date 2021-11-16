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

package v1beta1

import clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

const (
	//  ResourcePolicyReadyCondition reports the successful creation of a Resource Policy
	ResourcePolicyReadyCondition clusterv1.ConditionType = "ResourcePolicyReady"

	// ResourcePolicyCreationFailedReason used when any errors occur during ResourcePolicy creation
	ResourcePolicyCreationFailedReason = "ResourcePolicyCreationFailed"
)

const (
	// ClusterNetworkReadyCondition reports the successful provision of a Cluster Network
	ClusterNetworkReadyCondition clusterv1.ConditionType = "ClusterNetworkReady"

	// ClusterNetworkProvisionStarted is used when waiting for Cluster Network to be Ready
	ClusterNetworkProvisionStartedReason = "ClusterNetworkProvisionStarted"
	// ClusterNetworkProvisionFailedReason is used when any errors occur during network provision
	ClusterNetworkProvisionFailedReason = "ClusterNetworkProvisionFailed"
)

const (
	// LoadBalancerReadyCondition reports the successful reconciliation of a static control plane endpoint
	LoadBalancerReadyCondition clusterv1.ConditionType = "LoadBalancerReady"

	// LoadBalancerCreationFailedReason is used when load balancer related resources creation fails
	LoadBalancerCreationFailedReason = "LoadBalancerCreationFailed"
	// WaitingForLoadBalancerIPReason is used when waiting for load balancer IP to exist
	WaitingForLoadBalancerIPReason = "WaitingForLoadBalancerIP"
)
