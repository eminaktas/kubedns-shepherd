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

package webhook_controller

import (
	"fmt"
	"time"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/controller"
	"github.com/eminaktas/kubedns-shepherd/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilptr "k8s.io/utils/ptr"
)

var _ = Describe("Pods Webhook Controller", Ordered, func() {
	const timeout = 10 * time.Second
	const interval = time.Millisecond * 250
	const podGenerateName = "test-pod-"

	var ns *corev1.Namespace
	var pod *corev1.Pod
	var dnsclass *configv1alpha1.DNSClass
	Context("When creating and updating Pods", func() {
		BeforeAll(func() {
			By("Create kubeadm-config")
			Expect(utils.CreateKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
			By("Create kubelet-config")
			Expect(utils.CreateKubeletConfigMap(ctx, k8sClient)).Should(Succeed())
		})

		AfterAll(func() {
			By("Delete kubelet-config")
			Expect(utils.DeleteKubeletConfigMap(ctx, k8sClient)).Should(Succeed())
			By("Delete kubeadm-config")
			Expect(utils.DeleteKubeADMConfigMap(ctx, k8sClient)).Should(Succeed())
		})

		BeforeEach(func() {
			// Create test namespace before each test.
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-ns-",
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			// Wait until namespace created.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns))
			}, timeout, interval).Should(Succeed())
		})

		AfterEach(func() {
			// Removes pod, namespace and DNSClass after each test.
			By("Cleanup the Pod")
			err := k8sClient.Delete(ctx, pod)
			Expect(err).ShouldNot(HaveOccurred())
			By("Cleanup the Namespace")
			err = k8sClient.Delete(ctx, ns)
			Expect(err).ShouldNot(HaveOccurred())
			By("Cleanup the DNSClass instance")
			Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			ns, pod, dnsclass = &corev1.Namespace{}, &corev1.Pod{}, &configv1alpha1.DNSClass{}
		})

		It("should see DNSClass is created and the configuration applied to pod with no dns config defined", func() {
			By("Create a DNSClass")
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("2")},
					{Name: "edns0"},
				},
			}
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSConfig:          dnsconfig,
					DNSPolicy:          corev1.DNSNone,
					AllowedNamespaces:  []string{ns.Name},
					DisabledNamespaces: []string{"kube-system"},
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet},
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Wait until DNSClass is reconciled")
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[controller.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Create a Pod")
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podGenerateName,
					Namespace:    ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:stable",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
			By("Get the Pod object")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(pod.Spec.DNSPolicy).Should(Equal(dnsclass.Spec.DNSPolicy))
			// Check configured DNSClass name
			Expect(pod.GetAnnotations()[DNSClassName]).Should(Equal(dnsclass.Name))
		})

		It("should see DNSClass is created and the configuration applied to pod with discovered fields", func() {
			By("Create a DNSClass")
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"svc.{{ .clusterDomain }}", "{{ .podNamespace }}.svc.{{ .dnsDomain }}", "{{ .clusterName }}"},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("2")},
					{Name: "edns0"},
				},
			}
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSConfig:          dnsconfig,
					DNSPolicy:          corev1.DNSNone,
					AllowedNamespaces:  []string{ns.Name},
					DisabledNamespaces: []string{"kube-system"},
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet},
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Wait until DNSClass is reconciled")
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[controller.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Create a Pod")
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podGenerateName,
					Namespace:    ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:stable",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
			By("Get the Pod object")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(pod.Spec.DNSPolicy).Should(Equal(dnsclass.Spec.DNSPolicy))
			Expect(pod.GetAnnotations()[DNSClassName]).Should(Equal(dnsclass.Name))
			Expect(pod.Spec.DNSConfig).Should(Equal(&corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches: []string{fmt.Sprintf("svc.%s", utils.ClusterDomain),
					fmt.Sprintf("%s.svc.%s", pod.Namespace, utils.ClusterDomain),
					utils.ClusterName,
				},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("2")},
					{Name: "edns0"},
				},
			},
			))
		})

		It("should fail to see DNSClass configuration applied to pod", func() {
			By("Create a Pod")
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podGenerateName,
					Namespace:    ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:stable",
						},
					},
					DNSPolicy: corev1.DNSClusterFirst,
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
			By("Get the Pod object")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(pod.Spec.DNSPolicy).Should(Equal(corev1.DNSClusterFirst))
		})

		It("should fail to see DNSClass configuration applied to pod due to namespace is not allowed", func() {
			By("Create a DNSClass")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy:          corev1.DNSNone,
					AllowedNamespaces:  []string{"dummy-namespace"},
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet, corev1.DNSDefault},
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Wait until DNSClass is reconciled")
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[controller.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Create a Pod")
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podGenerateName,
					Namespace:    ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:stable",
						},
					},
					DNSPolicy: corev1.DNSDefault,
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
			By("Get the Pod object")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(pod.Spec.DNSPolicy).Should(Equal(corev1.DNSDefault))
		})

		It("should fail to see DNSClass configuration applied to pod due to DNSPolicy is not allowed", func() {
			By("Create a DNSClass")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy:          corev1.DNSNone,
					AllowedNamespaces:  []string{ns.Name},
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone},
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			By("Wait until DNSClass is reconciled")
			Eventually(func(g Gomega) {
				g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				g.Expect(dnsclass.GetAnnotations()[controller.IsReconciled]).Should(Equal("true"))
			}, timeout, interval).Should(Succeed())
			By("Create a Pod")
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podGenerateName,
					Namespace:    ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:stable",
						},
					},
					DNSPolicy: corev1.DNSDefault,
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
			By("Get the Pod object")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(pod.Spec.DNSPolicy).Should(Equal(corev1.DNSDefault))
		})

		It("should fail to find available DNSClass due to reconciliation", func() {
			By("Create a DNSClass")
			dnsclass = &configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSPolicy:          corev1.DNSNone,
					AllowedNamespaces:  []string{ns.Name},
					AllowedDNSPolicies: []corev1.DNSPolicy{corev1.DNSNone, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet, corev1.DNSDefault},
				},
			}
			Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			// Don't wait until reconciliation completed
			By("Create a Pod")
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podGenerateName,
					Namespace:    ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:stable",
						},
					},
					DNSPolicy: corev1.DNSDefault,
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
			By("Get the Pod object")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(pod.Spec.DNSPolicy).Should(Equal(corev1.DNSDefault))
		})
	})
})
