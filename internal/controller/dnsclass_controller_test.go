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
	"k8s.io/apimachinery/pkg/types"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/test/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

			By("Creating prerequisite ConfigMaps required for DNSClass")
			Expect(utils.CreateKubeADMConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to create kubeadm-config")
			Expect(utils.CreateKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to create kubelet-config")
		})

		AfterEach(func() {
			By(fmt.Sprintf("Cleaning up the DNSClass(%s) instance", dnsclass.Name))
			Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to delete DNSClass")

			By("Cleaning up ConfigMaps required for DNSClass")
			Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubeadm-config")
			Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")

			// Ensure DNSClass has been deleted
			Eventually(func(g Gomega) {
				err := utils.GetDNSClass(ctx, k8sClient, dnsclass)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "DNSClass not properly deleted")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			// Reset DNSClass variable for the next test
			dnsclass = nil
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
				ClusterName:   utils.ClusterName,
				DNSDomain:     utils.ClusterDomain,
			}

			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields mismatch")
		})

		It("should not populate discovered field but DNSClass should be available", func() {
			By("Removing kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubeadm-config")
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubeadm-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

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
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be nil due to missing ConfigMaps")
		})

		It("should fail to reconcile DNSClass due to missing discovered fields", func() {
			By("Removing kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubeadm-config")
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubeadm-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Creating a DNSClass with templating keys")
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"svc.{{ .clusterDomain }}", "ns.svc.{{ .dnsDomain }}", "{{ .clusterName }}"},
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

		It("should fail to discover nameservers, cluster domain, cluster name, and DNS domain", func() {
			By("Removing kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubeadm-config")
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubeadm-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

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
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be empty")
		})

		It("should fail to unmarshal ConfigMaps", func() {
			By("Removing kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubeadm-config")
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubeadm-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Creating faulty kubeadm ConfigMap")
			kubeadmConfig := `apiVersion: kubeadm.k8s.io/v1beta3
			kind: ClusterConfiguration
			`
			kubeadmcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubeadm-config",
					Namespace: metav1.NamespaceSystem,
				},
				Data: map[string]string{
					"ClusterConfiguration": kubeadmConfig,
				},
			}
			Expect(k8sClient.Create(ctx, kubeadmcm)).Should(Succeed())

			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      kubeadmcm.Name,
					Namespace: kubeadmcm.Namespace,
				}, kubeadmcm)).Should(Succeed())
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Creating a DNSClass")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")

			// Continuously check that the DNSClass reaches 'Ready' state
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to get DNSClass for validation")
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady), "DNSClass did not reach 'Ready' state")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField), "Discovered fields should be empty")
		})

		It("should fail to get networking, clusterName, clusterDomain and clusterDNS from ConfigMaps", func() {
			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Removing kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(Equal(true))
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Creating faulty kubeadm ConfigMap")
			kubeadmConfig := `apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
Invalidnetworking:
  dnsDomain: adnsdomain`
			kubeadmcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubeadm-config",
					Namespace: metav1.NamespaceSystem,
				},
				Data: map[string]string{
					"ClusterConfiguration": kubeadmConfig,
				},
			}
			Expect(k8sClient.Create(ctx, kubeadmcm)).Should(Succeed())

			By("Creating faulty kubelet ConfigMap")
			kubeletConfig := `apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration`
			kubeletcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubelet-config",
					Namespace: metav1.NamespaceSystem,
				},
				Data: map[string]string{
					"kubelet": kubeletConfig,
				},
			}
			Expect(k8sClient.Create(ctx, kubeletcm)).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      kubeadmcm.Name,
					Namespace: kubeadmcm.Namespace,
				}, kubeadmcm)).Should(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      kubeletcm.Name,
					Namespace: kubeletcm.Namespace,
				}, kubeletcm)).Should(Succeed())
			}, pollingTimeout, pollingInterval).Should(Succeed())

			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady))
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that kubelet discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField))
		})

		It("should fail to get dnsDomain from ConfigMaps", func() {
			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Removing kubeadm-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
				err := utils.GetKubeADMConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(Equal(true))
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Creating faulty kubeadm ConfigMap")
			kubeadmConfig := `apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
networking:
  InvaliddnsDomain: adnsdomain`
			kubeadmcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubeadm-config",
					Namespace: metav1.NamespaceSystem,
				},
				Data: map[string]string{
					"ClusterConfiguration": kubeadmConfig,
				},
			}
			Expect(k8sClient.Create(ctx, kubeadmcm)).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      kubeadmcm.Name,
					Namespace: kubeadmcm.Namespace,
				}, kubeadmcm)).Should(Succeed())

			}, pollingTimeout, pollingInterval).Should(Succeed())

			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady))
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that kubelet discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
				ClusterName:   "",
				DNSDomain:     "",
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField))
		})

		It("should fail to find a key in ConfigMap", func() {
			By("Removing kubelet-config")
			Eventually(func(g Gomega) {
				g.Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed(), "Failed to delete kubelet-config")
				err := utils.GetKubeletConfigMap(ctx, k8sClient)
				g.Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "kubelet-config still exists")
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Creating kubelet ConfigMap with wrong key")
			kubeletConfig := `apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration`
			kubeletcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubelet-config",
					Namespace: metav1.NamespaceSystem,
				},
				Data: map[string]string{
					"InvalidkubeletKey": kubeletConfig,
				},
			}
			Expect(k8sClient.Create(ctx, kubeletcm)).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      kubeletcm.Name,
					Namespace: kubeletcm.Namespace,
				}, kubeletcm)).Should(Succeed())
			}, pollingTimeout, pollingInterval).Should(Succeed())

			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy: corev1.DNSNone,
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())

			By(fmt.Sprintf("Syncing the DNSClass(%s) object", dnsclass.Name))
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.Status.State).Should(Equal(configv1alpha1.StateReady))
			}, pollingTimeout, pollingInterval).Should(Succeed())

			By("Validating that kubelet discovered fields remain unpopulated")
			expectedDiscoveredField := &configv1alpha1.DiscoveredFields{
				Nameservers:   nil,
				ClusterDomain: "",
				ClusterName:   utils.ClusterName,
				DNSDomain:     utils.ClusterDomain,
			}
			Expect(dnsclass.Status.DiscoveredFields).Should(Equal(expectedDiscoveredField))
		})
	})
})
