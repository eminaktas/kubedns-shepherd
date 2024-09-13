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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/common"
	"github.com/eminaktas/kubedns-shepherd/test/utils"
)

var _ = Describe("DNSClass Controller", Ordered, func() {
	const (
		timeout       = time.Second * 10
		interval      = time.Millisecond * 250
		dnsclassName  = "test-dnsclass"
		clusterDomain = "cluster.local"
		clusterDNS    = "10.96.0.10"
		clusterName   = "test-cluster"
		kubeadmName   = "kubeadm-config"
		kubeletName   = "kubelet-config"
	)
	var (
		ctx                    = context.Background()
		dnsclassNamespacedName = types.NamespacedName{
			Name: dnsclassName,
		}
		kubeadmNamespacedName = types.NamespacedName{
			Name:      kubeadmName,
			Namespace: metav1.NamespaceSystem,
		}
		kubeletNamespacedName = types.NamespacedName{
			Name:      kubeletName,
			Namespace: metav1.NamespaceSystem,
		}
		kubeadmConfig = fmt.Sprintf(`
apiVersion: kubeadm.k8s.io/v1beta3
clusterName: %s
kind: ClusterConfiguration
kubernetesVersion: v1.30.0
networking:
  dnsDomain: %s
`, clusterName, clusterDomain)
		kubeletConfig = fmt.Sprintf(`
apiVersion: kubelet.config.k8s.io/v1beta1
clusterDNS:
- %s
clusterDomain: %s
kind: KubeletConfiguration
`, clusterDNS, clusterDomain)
		allowedNamespaces        = []string{"default"}
		updatedallowedNamespaces = []string{"default", "another-namespace"}
		disabledNamespaces       = []string{"kube-system"}
		allowedDNSPolicies       = []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet}
		nameservers              = []string{clusterDNS}
		searches                 = []string{fmt.Sprintf("svc.%s", clusterDomain), clusterDomain}
	)
	Context("When creating, reconciling and removing a DNSClass", func() {
		BeforeAll(func() {
			By("Create kubeadm-config")
			kubeadmcm := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, kubeadmNamespacedName, kubeadmcm)
			if err != nil && errors.IsNotFound(err) {
				kubeadmcm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      kubeadmName,
						Namespace: metav1.NamespaceSystem,
					},
					Data: map[string]string{
						"ClusterConfiguration": kubeadmConfig,
					},
				}
				Expect(k8sClient.Create(ctx, kubeadmcm)).To(Succeed())
			}
			By("Create kubelet-config")
			kubeletcm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, kubeletNamespacedName, kubeletcm)
			if err != nil && errors.IsNotFound(err) {
				kubeletcm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      kubeletName,
						Namespace: metav1.NamespaceSystem,
					},
					Data: map[string]string{
						"kubelet": kubeletConfig,
					},
				}
				Expect(k8sClient.Create(ctx, kubeletcm)).To(Succeed())
			}
		})

		AfterEach(func() {
			dnsclass := &configv1alpha1.DNSClass{}
			err := k8sClient.Get(ctx, dnsclassNamespacedName, dnsclass)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the DNSClass instance")
			Expect(k8sClient.Delete(ctx, dnsclass)).To(Succeed())
		})

		It("should see DNSClass is created and validated specs", func() {
			By("Create a DNSClass")
			dnsConfig := &corev1.PodDNSConfig{
				Nameservers: nameservers,
				Searches:    searches,
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utils.StringPtr("2")},
					{Name: "edns0"},
				},
			}
			dnsclass := &configv1alpha1.DNSClass{}
			err := k8sClient.Get(ctx, dnsclassNamespacedName, dnsclass)
			if err != nil && errors.IsNotFound(err) {
				resource := &configv1alpha1.DNSClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: dnsclassName,
					},
					Spec: configv1alpha1.DNSClassSpec{
						DNSConfig:          dnsConfig,
						DNSPolicy:          corev1.DNSNone,
						AllowedNamespaces:  allowedNamespaces,
						DisabledNamespaces: disabledNamespaces,
						AllowedDNSPolicies: allowedDNSPolicies,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
			By("Get the created DNSClass")
			checkDNSclass := &configv1alpha1.DNSClass{}
			Expect(k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)).To(Succeed())
			By("Check the Spec field in the DNSClass")
			dnsclassSpec := configv1alpha1.DNSClassSpec{
				DNSConfig:          dnsConfig,
				DNSPolicy:          corev1.DNSNone,
				AllowedNamespaces:  allowedNamespaces,
				DisabledNamespaces: disabledNamespaces,
				AllowedDNSPolicies: allowedDNSPolicies,
			}
			Expect(checkDNSclass.Spec).To(Equal(dnsclassSpec))
		})

		It("should see discovered fields is populated in DNSClass", func() {
			By("Create a DNSClass")
			dnsclass := &configv1alpha1.DNSClass{}
			err := k8sClient.Get(ctx, dnsclassNamespacedName, dnsclass)
			if err != nil && errors.IsNotFound(err) {
				resource := &configv1alpha1.DNSClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: dnsclassName,
					},
					Spec: configv1alpha1.DNSClassSpec{
						DNSConfig:          &corev1.PodDNSConfig{},
						DNSPolicy:          corev1.DNSNone,
						AllowedNamespaces:  allowedNamespaces,
						DisabledNamespaces: disabledNamespaces,
						AllowedDNSPolicies: allowedDNSPolicies,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
			By("Get the DNSClass")
			checkDNSclass := &configv1alpha1.DNSClass{}
			err = k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)
			Expect(err).NotTo(HaveOccurred())
			// We must wait until the DNSClass reconciled.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)).To(Succeed())
				g.Expect(checkDNSclass.GetAnnotations()[common.IsReconciled]).To(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Check discovered fields")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nameservers,
				ClusterDomain: clusterDomain,
				ClusterName:   clusterName,
				DNSDomain:     clusterDomain,
			}
			Expect(checkDNSclass.Status.DiscoveredFields).To(Equal(expectedDiscoveredField))
		})

		It("should create a DNSClass with dnsPolicy `Default` and discovered fields musn't populated", func() {
			By("Create a DNSClass")
			dnsclass := &configv1alpha1.DNSClass{}
			err := k8sClient.Get(ctx, dnsclassNamespacedName, dnsclass)
			if err != nil && errors.IsNotFound(err) {
				resource := &configv1alpha1.DNSClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: dnsclassName,
					},
					Spec: configv1alpha1.DNSClassSpec{
						DNSPolicy:          corev1.DNSDefault,
						AllowedNamespaces:  allowedNamespaces,
						DisabledNamespaces: disabledNamespaces,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
				By("Get the DNSClass")
				checkDNSclass := &configv1alpha1.DNSClass{}
				err = k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)
				Expect(err).NotTo(HaveOccurred())
				By("Check discovered fields and DNS Policy")
				Expect(checkDNSclass.Spec.DNSPolicy).To(Equal(corev1.DNSDefault))
				var discoveredFields *configv1alpha1.DiscoveredFields
				Expect(checkDNSclass.Status.DiscoveredFields).To(Equal(discoveredFields))
			}
		})

		It("should update DNSClass", func() {
			By("Create a DNSClass")
			dnsConfig := &corev1.PodDNSConfig{
				Nameservers: nameservers,
				Searches:    searches,
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utils.StringPtr("2")},
					{Name: "edns0"},
				},
			}
			dnsclass := &configv1alpha1.DNSClass{}
			err := k8sClient.Get(ctx, dnsclassNamespacedName, dnsclass)
			if err != nil && errors.IsNotFound(err) {
				resource := &configv1alpha1.DNSClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: dnsclassName,
					},
					Spec: configv1alpha1.DNSClassSpec{
						DNSConfig:          dnsConfig,
						DNSPolicy:          corev1.DNSNone,
						AllowedNamespaces:  allowedNamespaces,
						DisabledNamespaces: disabledNamespaces,
						AllowedDNSPolicies: allowedDNSPolicies,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
			By("Get the DNSClass")
			checkDNSclass := &configv1alpha1.DNSClass{}
			// We should keep loking for kubedns-shepherd.io/is-reconciled as true
			// Otherwise, when resource is updated the test will fail
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)).To(Succeed())
				g.Expect(checkDNSclass.GetAnnotations()[common.IsReconciled]).To(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Update DNSClass")
			checkDNSclass.Spec.AllowedNamespaces = updatedallowedNamespaces
			Expect(k8sClient.Update(ctx, checkDNSclass)).To(Succeed())
			By("Get the updated DNSClass")
			checkDNSclassv2 := &configv1alpha1.DNSClass{}
			Expect(k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclassv2)).To(Succeed())
			Expect(checkDNSclassv2.Spec.AllowedNamespaces).To(Equal(updatedallowedNamespaces))
		})

		// Please keep this last test unless you need kubelet-config and kubeadm-config resources.
		It("should fail to discover nameservers, cluster domain, cluster name and dns domain", func() {
			By("Remove kubelet-config")
			kubeletcm := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, kubeletNamespacedName, kubeletcm)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Delete(ctx, kubeletcm)).To(Succeed())

			By("Remove kubeadm-config")
			kubeadmcm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, kubeadmNamespacedName, kubeadmcm)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Delete(ctx, kubeadmcm)).To(Succeed())

			By("Create a DNSClass")
			dnsclass := &configv1alpha1.DNSClass{}
			err = k8sClient.Get(ctx, dnsclassNamespacedName, dnsclass)
			if err != nil && errors.IsNotFound(err) {
				resource := &configv1alpha1.DNSClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: dnsclassName,
					},
					Spec: configv1alpha1.DNSClassSpec{
						DNSConfig:          &corev1.PodDNSConfig{},
						DNSPolicy:          corev1.DNSNone,
						AllowedNamespaces:  allowedNamespaces,
						DisabledNamespaces: disabledNamespaces,
						AllowedDNSPolicies: allowedDNSPolicies,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
			By("Get the DNSClass")
			checkDNSclass := &configv1alpha1.DNSClass{}
			err = k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)
			Expect(err).NotTo(HaveOccurred())
			// We must wait until the DNSClass reconciled.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, dnsclassNamespacedName, checkDNSclass)).To(Succeed())
				g.Expect(checkDNSclass.GetAnnotations()[common.IsReconciled]).To(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Check discovered fields")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(checkDNSclass.Status.DiscoveredFields).To(Equal(expectedDiscoveredField))
		})
	})
})
