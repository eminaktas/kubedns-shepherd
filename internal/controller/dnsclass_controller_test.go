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

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/common"
	"github.com/eminaktas/kubedns-shepherd/test/utils"
	utilptr "k8s.io/utils/ptr"
)

var _ = Describe("DNSClass Controller", Ordered, func() {
	const timeout = time.Second * 10
	const interval = time.Millisecond * 250

	// Each test uses different DNSClass
	var dnsclass *configv1alpha1.DNSClass

	Context("When creating, updating, reconciling and removing a DNSClass", func() {
		ctx := context.Background()
		BeforeAll(func() {
			// Create prerequisite ConfigMaps required for DNSClass.
			By("Create kubeadm-config")
			Expect(utils.CreateKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
			By("Create kubelet-config")
			Expect(utils.CreateKubeletConfigMap(ctx, k8sClient)).Should(Succeed())
		})

		// Remove ConfigMaps created for DNSClass.
		AfterAll(func() {
			By("Delete kubelet-config")
			Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed())
			By("Delete kubeadm-config")
			Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
		})

		// Remove the DNSClass instance.
		AfterEach(func() {
			By(fmt.Sprintf("Cleanup the DNSClass(%s) instance", dnsclass.Name))
			Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			Eventually(func(g Gomega) {
				err := utils.GetDNSClass(ctx, k8sClient, dnsclass)
				g.Expect(apierrors.IsNotFound(err)).Should(Equal(true))
			}, timeout, interval).Should(Succeed())
			dnsclass = &configv1alpha1.DNSClass{}
		})

		It("should see DNSClass is created and validated specs", func() {
			By("Create a DNSClass")
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("2")},
					{Name: "edns0"},
				},
			}
			dnsclassSpec := configv1alpha1.DNSClassSpec{
				DNSConfig:          dnsconfig,
				DNSPolicy:          corev1.DNSNone,
				AllowedNamespaces:  []string{"default"},
				DisabledNamespaces: []string{"kube-system"},
				AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet},
			}
			dnsclass = &configv1alpha1.DNSClass{
				Spec: dnsclassSpec,
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			// Sync and validate the created DNSClass object.
			By(fmt.Sprintf("Sync the DNSClass(%s) object", dnsclass.Name))
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check the Spec fields")
			Expect(dnsclass.Spec).Should(Equal(dnsclassSpec))
		})

		It("should see discovered fields is populated in DNSClass", func() {
			By("Create a DNSClass")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By(fmt.Sprintf("Sync the DNSClass(%s) object", dnsclass.Name))
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			// Continuously check for the `kubedns-shepherd.io/is-reconciled` annotation to be "true".
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[common.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			// Validate that the discovered fields are populated as expected.
			By("Check discovered fields")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   []string{utils.ClusterDNS},
				ClusterDomain: utils.ClusterDomain,
				ClusterName:   utils.ClusterName,
				DNSDomain:     utils.ClusterDomain,
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField))
		})

		It("should create a DNSClass with dnsPolicy `Default` and discovered fields musn't populated", func() {
			By("Create a DNSClass")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSDefault,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			// Check the discovered fields and DNS Policy
			By(fmt.Sprintf("Sync the DNSClass(%s) object", dnsclass.Name))
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Check discovered fields and DNS Policy")
			Expect(dnsclass.Spec.DNSPolicy).Should(Equal(corev1.DNSDefault))
			// Validate that discovered fields are empty.
			var discoveredFields *configv1alpha1.DiscoveredFields
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(discoveredFields))
		})

		It("should update DNSClass", func() {
			By("Create a DNSClass")
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("2")},
					{Name: "edns0"},
				},
			}
			dnsclassSpec := configv1alpha1.DNSClassSpec{
				DNSConfig:          dnsconfig,
				DNSPolicy:          corev1.DNSNone,
				AllowedNamespaces:  []string{"default"},
				DisabledNamespaces: []string{"kube-system"},
				AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet},
			}
			dnsclass = &configv1alpha1.DNSClass{
				Spec: dnsclassSpec,
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())

			By(fmt.Sprintf("Sync the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[common.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By(fmt.Sprintf("Update DNSClass(%s)", dnsclass.Name))
			updatedAllowedNamespaces := []string{"default", "another-namespace"}
			// Re-fetch the DNSClass object before update
			time.Sleep(1 * time.Second)
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			dnsclass.Spec.AllowedNamespaces = updatedAllowedNamespaces
			Expect(k8sClient.Update(ctx, dnsclass)).Should(Succeed())
			By(fmt.Sprintf("Get the updated DNSClass(%s)", dnsclass.Name))
			Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			Expect(dnsclass.Spec.AllowedNamespaces).Should(Equal(updatedAllowedNamespaces))
		})

		// This test ensures the DNSClass fails to discover fields when the required ConfigMaps are removed.
		// It's important to keep this as the last test since it deletes the ConfigMaps.
		It("should fail to discover nameservers, cluster domain, cluster name and dns domain", func() {
			By("Remove kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(Equal(true))
			}, timeout, interval).Should(Succeed())
			By("Remove kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed())
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(Equal(true))
			}, timeout, interval).Should(Succeed())
			By("Create a DNSClass")
			allowedNamespaces := []string{"default"}
			disabledNamespaces := []string{"kube-system"}
			allowedDNSPolicies := []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet}
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSConfig:          &corev1.PodDNSConfig{},
					DNSPolicy:          corev1.DNSNone,
					AllowedNamespaces:  allowedNamespaces,
					DisabledNamespaces: disabledNamespaces,
					AllowedDNSPolicies: allowedDNSPolicies,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By(fmt.Sprintf("Sync the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[common.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			// Validate that the discovered fields remain unpopulated.
			By("Check discovered fields")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField))
		})
	})
})
