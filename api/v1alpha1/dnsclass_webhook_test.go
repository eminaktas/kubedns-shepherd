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

package v1alpha1

import (
	"context"
	"fmt"

	"github.com/eminaktas/kubedns-shepherd/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("DNSClass Webhook", Ordered, func() {
	var dnsclass *DNSClass

	Context("When creating DNSClass with Defaulting Webhook", func() {
		AfterEach(func() {
			By("Cleaning up the DNSClass instance")
			if dnsclass != nil {
				Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				// Reset DNSClass variable for the next test
				dnsclass = nil
			}
		})

		It("Should apply default values when not provided", func() {
			By("Creating DNSClass with no fields set")
			dnsclass = &DNSClass{}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())

			By("Verifying default values are applied")
			Expect(dnsclass.Spec.DNSPolicy).Should(Equal(corev1.DNSNone), "DNSPolicy should default to DNSNone")
			Expect(dnsclass.Spec.AllowedDNSPolicies).Should(Equal(DefaultDNSPolicies), "AllowedDNSPolicies should default to valid defaults")
		})

		It("Should allow updating DNSClass fields", func() {
			By("Creating a valid DNSClass")
			dnsclass = &DNSClass{}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())

			By("Updating the AllowedNamespaces field")
			updatedAllowedNamespaces := []string{"default", "another-namespace"}
			dnsclass.Spec.AllowedNamespaces = updatedAllowedNamespaces
			Expect(k8sClient.Update(ctx, dnsclass)).Should(Succeed())

			By("Verifying that the DNSClass was updated correctly")
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			Expect(dnsclass.Spec.AllowedNamespaces).Should(Equal(updatedAllowedNamespaces), "AllowedNamespaces should be updated correctly")
		})
	})

	Context("When creating DNSClass under Validating Webhook", func() {
		// Helper to create DNSClass and validate reconciliation for validating
		createAndValidateDNSClass := func() {
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")
			By("Syncing DNSClass")
			Expect(dnsclass.GetName()).Should(BeEmpty())
		}

		AfterEach(func() {
			By("Cleaning up the DNSClass instance")
			if dnsclass != nil {
				Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				// Reset DNSClass variable for the next test
				dnsclass = nil
			}
		})

		It("should reject invalid AllowedDNSPolicies", func() {
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, "InvalidDNSPolicy"},
				},
			}
			createAndValidateDNSClass()
		})

		It("should reject invalid DNSPolicy", func() {
			By("Creating DNSClass with an invalid DNSPolicy")
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					DNSPolicy: "InvalidDNSPolicy",
				},
			}
			createAndValidateDNSClass()
		})

		It("should reject DNSConfig when DNSPolicy is not `None`", func() {
			By("Creating DNSClass with DNSConfig but without DNSPolicy None")
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options:     []corev1.PodDNSConfigOption{{Name: "edns0"}},
			}
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					DNSPolicy: corev1.DNSDefault,
					DNSConfig: dnsconfig,
				},
			}
			createAndValidateDNSClass()
		})

		It("should reject DNSConfig with invalid template keys", func() {
			By("Creating DNSClass with invalid template keys in DNSConfig.Searches")
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"svc.{{ .podNamespace }}", "{{ .clusterDomainInvalid }}"},
			}
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
					DNSConfig: dnsconfig,
				},
			}
			createAndValidateDNSClass()
		})
	})

	Context("When calling functions directly", func() {
		It("should return error when calling Default function with different object", func() {
			dnsCustomDefault := &DNSClassCustomDefaulter{}
			err := dnsCustomDefault.Default(context.TODO(), &corev1.Pod{})
			Expect(err).Should(HaveOccurred())
		})

		It("should return error when calling ValidateCreate with different object", func() {
			dnsCustomValidator := &DNSClassCustomValidator{}
			warn, err := dnsCustomValidator.ValidateCreate(context.TODO(), &corev1.Pod{})
			Expect(warn).Should(BeNil())
			Expect(err).Should(HaveOccurred())
		})

		It("should return error when calling ValidateUpdate with different object", func() {
			dnsCustomValidator := &DNSClassCustomValidator{}
			warn, err := dnsCustomValidator.ValidateUpdate(context.TODO(), nil, &corev1.Pod{})
			Expect(warn).Should(BeNil())
			Expect(err).Should(HaveOccurred())
		})

		It("should return error when calling ValidateDelete with different object", func() {
			dnsCustomValidator := &DNSClassCustomValidator{}
			warn, err := dnsCustomValidator.ValidateDelete(context.TODO(), &corev1.Pod{})
			Expect(warn).Should(BeNil())
			Expect(err).Should(BeNil())
		})
	})
})
