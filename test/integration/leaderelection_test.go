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
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	uuid "github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/manager"
)

// The purpose of this test is to start up a CAPI controller against a real API
// server and run Cluster tests
var _ = Describe("Controller leader election tests", func() {

	var (
		mgr     *TestManager
		mgrOpts *manager.Options
	)

	BeforeEach(func() {
		mgrOpts = &manager.Options{}
	})

	JustBeforeEach(func() {
		// Start the controller.
		mgrOpts.PodName = fmt.Sprintf("test-leaderelection-controller-%s", uuid.New())
		mgrOpts.LeaderElectionEnabled = true
		mgr = startControllerManager(*mgrOpts)
	})

	AfterEach(func() {
		stopControllerManager(mgr)
		mgr = nil
		mgrOpts = nil
	})

	Context("When Leader election is enabled", func() {
		It("the leader election configmap should exist", func() {
			_, err := mgr.client.Resource(configmapsResource).Namespace(mgr.GetControllerManagerNamespace()).Get(mgr.GetContext(), mgr.GetLeaderElectionID(), metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		It("the leader election configmap specifies a single leader", func() {
			output, err := mgr.client.Resource(configmapsResource).Namespace(mgr.GetControllerManagerNamespace()).Get(mgr.GetContext(), mgr.GetLeaderElectionID(), metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			annotations := output.GetAnnotations()
			leaderID, err := os.Hostname()
			Expect(err).NotTo(HaveOccurred())
			Expect(annotations["control-plane.alpha.kubernetes.io/leader"]).To(ContainSubstring(leaderID))
		})
	})
})
