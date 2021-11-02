// Copyright (c) 2019 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package vmoperator

const (
	metadataFormat = `
instance-id: "{{ .Hostname }}"
local-hostname: "{{ .Hostname }}"
{{ if .ControlPlaneEndpoint }}
controlPlaneEndpoint: "{{ .ControlPlaneEndpoint }}"
{{ end }}
`
	ControlPlaneVMClusterModuleGroupName = "control-plane-group"
	ClusterModuleNameAnnotationKey       = "vsphere-cluster-module-group"
	ProviderTagsAnnotationKey            = "vsphere-tag"
	ControlPlaneVMVMAntiAffinityTagValue = "CtrlVmVmAATag"
	WorkerVMVMAntiAffinityTagValue       = "WorkerVmVmAATag"
)
