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
	"fmt"

	"github.com/eminaktas/kubedns-shepherd/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("DNSClass Webhook", Ordered, func() {
	var dnsclass *DNSClass

	AfterEach(func() {
		By("Cleanup the DNSClass instance")
		Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
		dnsclass = &DNSClass{}
	})

	Context("When creating DNSClass under Defaulting Webhook", func() {
		It("Should fill in the default values", func() {
			By("Create DNSClass")
			dnsclass = &DNSClass{}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check default fields")
			Expect(dnsclass.Spec.DNSPolicy).Should(Equal(corev1.DNSNone))
			Expect(dnsclass.Spec.AllowedDNSPolicies).Should(Equal(defaultDNSPolicies))
		})
	})

	Context("When creating DNSClass under Validating Webhook", func() {
		It("Should deny if any of the allowedDNSPolicies is not valid", func() {
			By("Create DNSClass")
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, "NoneValidDNSPolicy"},
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check DNSClass name")
			Expect(dnsclass.GetName()).Should(BeEmpty())
		})

		It("Should deny if DNSPolicy is not valid", func() {
			By("Create DNSClass")
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					DNSPolicy: "NoneValidDNSPolicy",
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check DNSClass name")
			Expect(dnsclass.GetName()).Should(BeEmpty())
		})

		It("Should deny if DNSConfig is set without DNSPolicy `None`", func() {
			By("Create DNSClass")
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options: []corev1.PodDNSConfigOption{
					{Name: "edns0"},
				},
			}
			dnsclass = &DNSClass{
				Spec: DNSClassSpec{
					DNSPolicy: corev1.DNSDefault,
					DNSConfig: dnsconfig,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check DNSClass name")
			Expect(dnsclass.GetName()).Should(BeEmpty())
		})

		It("Should update DNSConfig", func() {
			By("Create DNSClass")
			dnsclass = &DNSClass{}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Update DNSClass")
			updatedAllowedNamespaces := []string{"default", "another-namespace"}
			dnsclass.Spec.AllowedNamespaces = updatedAllowedNamespaces
			Expect(k8sClient.Update(ctx, dnsclass)).Should(Succeed())
			By("Sync DNSClass")
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check allowedNamespaces")
			Expect(dnsclass.Spec.AllowedNamespaces).Should(Equal(updatedAllowedNamespaces))
		})
	})
})
