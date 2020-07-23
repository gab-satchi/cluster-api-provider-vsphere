/*
Copyright 2020 The Kubernetes Authors.
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

package v1alpha3

import (
	"testing"

	. "github.com/onsi/gomega"
)

var (
	biosUUID = "vsphere://42305f0b-dad7-1d3d-5727-0eafffffbbbfc"
)

//nolint
func TestVSphereVM_ValidateCreate(t *testing.T) {

	g := NewWithT(t)
	tests := []struct {
		name      string
		vSphereVM *VSphereVM
		wantErr   bool
	}{
		{
			name:      "preferredAPIServerCIDR set on creation ",
			vSphereVM: createVSphereVM("foo.com", "", "192.168.0.1/32", []string{}),
			wantErr:   true,
		},
		{
			name:      "ProviderID set on creation",
			vSphereVM: createVSphereVM("foo.com", biosUUID, "", []string{}),
			wantErr:   true,
		},
		{
			name:      "IPs are not in CIDR format",
			vSphereVM: createVSphereVM("foo.com", "", "", []string{"192.168.0.1/32", "192.168.0.3"}),
			wantErr:   true,
		},
		{
			name:      "successful VSphereVM creation",
			vSphereVM: createVSphereVM("foo.com", "", "", []string{"192.168.0.1/32", "192.168.0.3/32"}),
			wantErr:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.vSphereVM.ValidateCreate()
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

//nolint
func TestVSphereVM_ValidateUpdate(t *testing.T) {

	g := NewWithT(t)

	tests := []struct {
		name         string
		oldVSphereVM *VSphereVM
		vSphereVM    *VSphereVM
		wantErr      bool
	}{
		{
			name:         "ProviderID can be updated",
			oldVSphereVM: createVSphereVM("foo.com", "", "", []string{"192.168.0.1/32"}),
			vSphereVM:    createVSphereVM("foo.com", biosUUID, "", []string{"192.168.0.1/32"}),
			wantErr:      false,
		},
		{
			name:         "updating ips cannot be done",
			oldVSphereVM: createVSphereVM("foo.com", "", "", []string{"192.168.0.1/32"}),
			vSphereVM:    createVSphereVM("foo.com", biosUUID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}),
			wantErr:      true,
		},
		{
			name:         "updating server cannot be done",
			oldVSphereVM: createVSphereVM("foo.com", "", "", []string{"192.168.0.1/32"}),
			vSphereVM:    createVSphereVM("bar.com", biosUUID, "", []string{"192.168.0.1/32", "192.168.0.10/32"}),
			wantErr:      true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.vSphereVM.ValidateUpdate(tc.oldVSphereVM)
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func createVSphereVM(server string, biosUUID string, preferredAPIServerCIDR string, ips []string) *VSphereVM {
	VSphereVM := &VSphereVM{
		Spec: VSphereVMSpec{
			BiosUUID: biosUUID,
			VirtualMachineCloneSpec: VirtualMachineCloneSpec{
				Server: server,
				Network: NetworkSpec{
					PreferredAPIServerCIDR: preferredAPIServerCIDR,
					Devices:                []NetworkDeviceSpec{},
				},
			},
		},
	}
	for _, ip := range ips {
		VSphereVM.Spec.Network.Devices = append(VSphereVM.Spec.Network.Devices, NetworkDeviceSpec{
			IPAddrs: []string{ip},
		})
	}
	return VSphereVM
}