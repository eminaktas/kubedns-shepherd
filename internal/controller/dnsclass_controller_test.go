/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/test/utils"
	utilptr "k8s.io/utils/ptr"
)

var _ = Describe("DNSClass Controller", Ordered, func() {
	const (
		// Polling timeout for eventually blocks
		pollingTimeout = 10 * time.Second
		// Polling interval for checking conditions
		pollingInterval = 250 * time.Millisecond
	)

	// Each test uses different DNSClass
	var dnsclass *configv1alpha1.DNSClass

	Context("When creating, updating, reconciling and removing a DNSClass", func() {
		var ctx context.Context

		BeforeEach(func() {
			// Initialize context for each test case
			ctx = context.Background()
		})

		AfterEach(func() {
			if dnsclass != nil {
				By(fmt.Sprintf("Cleaning up the DNSClass(%s) instance", dnsclass.Name))
				Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to delete DNSClass")

				// Ensure DNSClass has been deleted
				Eventually(func(g Gomega) {
					err := utils.GetDNSClass(ctx, k8sClient, dnsclass)
					g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "DNSClass not properly deleted")
				}, pollingTimeout, pollingInterval).Should(Succeed())
			}

			// Reset DNSClass variable for the next test
			dnsclass = nil

			// Reset
			utils.MockResponse = nil
		})

		It("should create and validate DNSClass specs", func() {
			By("Creating a DNSClass with specific DNSConfig")
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("2")},
					{Name: "edns0"},
				},
			}

			// Define the expected DNSClass spec
			dnsclassSpec := configv1alpha1.DNSClassSpec{
				DNSConfig:          dnsconfig,
				DNSPolicy:          corev1.DNSNone,
				AllowedNamespaces:  []string{"default"},
				DisabledNamespaces: []string{"kube-system"},
				AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet},
			}

			// Initialize DNSClass object with spec
			dnsclass = &configv1alpha1.DNSClass{
				Spec: dnsclassSpec,
			}

			// Create the DNSClass
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to get DNSClass")

			By("Validating DNSClass Spec fields")
			Expect(dnsclass.Spec).Should(Equal(dnsclassSpec), "DNSClass spec mismatch")
		})

		It("should populate discovered fields in DNSClass", func() {
			By("Creating a DNSClass with DNSPolicy set to None")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}

			// Create the DNSClass
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to sync DNSClass")

			// Continuously check that the DNSClass reaches 'Ready' state
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to get DNSClass for validation")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady), "DNSClass did not reach 'Ready' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating discovered fields")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   []string{utils.ClusterDNS},
				ClusterDomain: utils.ClusterDomain,
			}

			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields mismatch")
		})

		It("should not populate discovered field but DNSClass should be available", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{},
			}

			By("Creating a DNSClass with only podNamespace templating key")
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"svc.cluster.local", "{{ .podNamespace }}.svc.cluster.local", "cluster.local"},
			}

			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
					DNSConfig: dnsconfig,
				},
			}

			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to sync DNSClass")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady), "DNSClass did not reach 'Error' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be nil due to missing ConfigMaps")
		})

		It("should fail to reconcile DNSClass due to missing discovered fields", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{
					"clusterDNS": []string{utils.ClusterDNS},
				},
			}

			By("Creating a DNSClass with templating keys")
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"svc.{{ .clusterDomain }}", "ns.svc.{{ .clusterDomain }}", "{{ .clusterDomain }}"},
			}

			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					AllowedNamespaces: []string{"ns"},
					DNSPolicy:         corev1.DNSNone,
					DNSConfig:         dnsconfig,
				},
			}

			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to sync DNSClass")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateError), "DNSClass did not reach 'Error' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that discovered fields remain nil")
			Expect(dnsclass.Status.DiscoveredFields).Should(BeNil(), "Discovered fields should be nil due to missing ConfigMaps")
		})

		It("should fail to test string and slice fields", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{
					"clusterDNS":    "random-string",
					"clusterDomain": []string{"random-string"},
				},
			}

			By("Creating a DNSClass with DNSPolicy set to None")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}

			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to sync DNSClass")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady), "DNSClass did not reach 'Ready' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be empty")
		})

		It("should fail to test string for slice field", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{
					"clusterDNS": []bool{true},
				},
			}

			By("Creating a DNSClass with DNSPolicy set to None")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}

			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to sync DNSClass")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady), "DNSClass did not reach 'Ready' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be empty")
		})

		It("should fail to test field due to unsupported target type", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{
					"dummy": []bool{true},
				},
			}

			dnsClassReconciler := DNSClassReconciler{
				Client: k8sClient,
				Config: utils.GetDummyConfig(mockServer.URL, k8sManager),
			}

			By("Validating that function failed")
			_, err := dnsClassReconciler.extractField(ctx, "dummy", "bool")
			Expect(err).Should(MatchError("unsupported target type: bool"))
		})

		It("should fail to test field due to failed create REST client", func() {
			By("Creating dummy config")
			config := utils.GetDummyConfig(mockServer.URL, k8sManager)
			config.ExecProvider = &clientcmdapi.ExecConfig{}
			config.AuthProvider = &clientcmdapi.AuthProviderConfig{}

			dnsClassReconciler := DNSClassReconciler{
				Client: k8sClient,
				Config: config,
			}

			By("Validating that function failed")
			_, err := dnsClassReconciler.fetchNodeProxyConfigz(ctx)
			Expect(err.Error()).Should(ContainSubstring("failed to create REST client"))
		})

		It("should fail to test field due to failed get raw data", func() {
			By("Creating dummy config")
			config := utils.GetDummyConfig(mockServer.URL, k8sManager)
			config.Host = "nil"

			dnsClassReconciler := DNSClassReconciler{
				Client: k8sClient,
				Config: config,
			}

			By("Validating that function failed")
			_, err := dnsClassReconciler.fetchNodeProxyConfigz(ctx)
			Expect(err.Error()).Should(ContainSubstring("failed to get raw data"))
		})

		It("should fail to test field due to unmarshal", func() {
			By("Tampering the configz response")
			utils.MockResponse = "nil"

			dnsClassReconciler := DNSClassReconciler{
				Client: k8sClient,
				Config: utils.GetDummyConfig(mockServer.URL, k8sManager),
			}

			By("Validating that function failed")
			_, err := dnsClassReconciler.fetchNodeProxyConfigz(ctx)
			Expect(err.Error()).Should(ContainSubstring("failed to unmarshal config"))
		})

		It("should fail to test field due to find kubeletconfig", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"dummy": true,
			}

			dnsClassReconciler := DNSClassReconciler{
				Client: k8sClient,
				Config: utils.GetDummyConfig(mockServer.URL, k8sManager),
			}

			By("Validating that function failed")
			_, err := dnsClassReconciler.fetchNodeProxyConfigz(ctx)
			Expect(err.Error()).Should(ContainSubstring("kubeletconfig field is not a map or is not found"))
		})

		It("should fail to find avaible node", func() {
			By("Removing dummy node")
			Expect(utils.DeleteNode(ctx, k8sClient)).Should(Succeed(), "Failed to remove the dummy node")

			By("Creating a DNSClass with DNSPolicy set to None")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}

			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to sync DNSClass")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady), "DNSClass did not reach 'Ready' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be empty")

			// Add back the node for other tests
			Expect(utils.AddNode(ctx, k8sClient)).Should(Succeed(), "Failed to add back the dummy node")
		})
	})
})
