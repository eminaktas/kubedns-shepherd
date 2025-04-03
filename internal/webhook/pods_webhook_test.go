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
	"context"
	"fmt"
	"time"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	utilptr "k8s.io/utils/ptr"
)

var _ = Describe("Pods Webhook Controller", Ordered, func() {
	const (
		pollingTimeout  = 10 * time.Second
		pollingInterval = 250 * time.Millisecond
		podGenerateName = "test-pod-"
	)

	var (
		ns       *corev1.Namespace
		pod      *corev1.Pod
		dnsclass *configv1alpha1.DNSClass
	)

	// Helper to create DNSClass and validate reconciliation
	createAndValidateDNSClass := func(dnsconfig *corev1.PodDNSConfig, dnsPolicy corev1.DNSPolicy, allowedNamespaces []string, disabledNamespaces []string, allowedDNSPolicies []corev1.DNSPolicy, expectedState string) {
		dnsclass = &configv1alpha1.DNSClass{
			Spec: configv1alpha1.DNSClassSpec{
				DNSConfig:          dnsconfig,
				DNSPolicy:          dnsPolicy,
				AllowedNamespaces:  allowedNamespaces,
				DisabledNamespaces: disabledNamespaces,
				AllowedDNSPolicies: allowedDNSPolicies,
			},
		}

		Expect(utils.CreateDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed(), "Failed to create DNSClass")
		By("Waiting until DNSClass is reconciled")
		Eventually(func(g Gomega) {
			g.Expect(utils.GetDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
			g.Expect(dnsclass.Status.State).Should(Equal(expectedState))
		}, pollingTimeout, pollingInterval).Should(Succeed())
	}

	// Helper to create Pod
	createPod := func(podDNSPolicy corev1.DNSPolicy, addOwnerRef, generateName bool) {
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:stable",
					},
				},
				DNSPolicy: podDNSPolicy,
			},
		}

		if addOwnerRef {
			pod.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion:         "apps/v1",
					BlockOwnerDeletion: utilptr.To(true),
					Controller:         utilptr.To(true),
					Name:               "test-pod-6c8cb99bb9",
					Kind:               "ReplicaSet",
					UID:                "f3b8dd69-4dff-44bd-8ad5-73668d82dcaa",
				},
			}
		}

		if generateName {
			pod.GenerateName = podGenerateName
		} else {
			pod.Name = podGenerateName + "0"
		}
		Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
	}

	validateDNSPolicy := func(expectedDNSPolicy corev1.DNSPolicy) {
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).Should(Succeed())
			g.Expect(pod.Spec.DNSPolicy).Should(Equal(expectedDNSPolicy))
		}, pollingTimeout, pollingInterval).Should(Succeed())
	}

	// Helper to create Pod and validate DNS policy
	createPodAndValidate := func(podDNSPolicy, expectedDNSPolicy corev1.DNSPolicy) {
		createPod(podDNSPolicy, false, true)
		validateDNSPolicy(expectedDNSPolicy)
	}

	// Helper to create Pod with static name and validate DNS Policy
	createPodwithNameAndValidate := func(podDNSPolicy, expectedDNSPolicy corev1.DNSPolicy) {
		createPod(podDNSPolicy, false, false)
		validateDNSPolicy(expectedDNSPolicy)
	}

	// Helper to create Pod with owner referance and validate DNS policy
	createPodWithOwnerRefAndValidate := func(podDNSPolicy, expectedDNSPolicy corev1.DNSPolicy) {
		createPod(podDNSPolicy, true, true)
		validateDNSPolicy(expectedDNSPolicy)
	}

	Context("When creating and updating Pods", func() {
		BeforeEach(func() {
			// Create test namespace before each test.
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-ns-",
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
		})

		AfterEach(func() {
			// Cleanup pod, namespace, and DNSClass after each test.
			By("Deleting resources")
			if pod != nil {
				Expect(k8sClient.Delete(ctx, pod)).Should(Succeed())
				pod = nil
			}
			if ns != nil {
				Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
				ns = nil
			}
			if dnsclass != nil {
				Expect(utils.DeleteDNSClass(ctx, k8sClient, dnsclass)).Should(Succeed())
				dnsclass = nil
			}
			// Reset
			utils.MockResponse = nil
		})

		It("should apply DNSClass configuration to pod without DNS config defined", func() {
			dnsconfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches:    []string{fmt.Sprintf("svc.%s", utils.ClusterDomain), utils.ClusterDomain},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("3")},
					{Name: "edns0"},
				},
			}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, nil, nil, nil, configv1alpha1.StateReady)

			// Create Pod and verify
			createPodAndValidate(corev1.DNSClusterFirst, dnsclass.Spec.DNSPolicy)

			By("Validating Pod annotations")
			Expect(pod.GetAnnotations()[DNSClassName]).Should(Equal(dnsclass.Name))
		})

		It("should apply DNSClass with discovered fields to pod", func() {
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"{{ .podNamespace }}.svc.{{ .clusterDomain }}", "svc.{{ .clusterDomain }}", "{{ .clusterDomain }}"},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("3")},
					{Name: "edns0"},
				},
			}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, []string{ns.Name}, nil, nil, configv1alpha1.StateReady)

			// Create Pod and verify
			createPodwithNameAndValidate(corev1.DNSClusterFirst, dnsclass.Spec.DNSPolicy)

			By("Validating PodDNSConfig")
			expectedDNSConfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches: []string{
					fmt.Sprintf("%s.svc.%s", pod.Namespace, utils.ClusterDomain),
					fmt.Sprintf("svc.%s", utils.ClusterDomain),
					utils.ClusterDomain,
				},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("3")},
					{Name: "edns0"},
				},
			}
			Expect(pod.Spec.DNSConfig).Should(Equal(expectedDNSConfig))
		})

		It("should apply DNSClass to pods with owner referance", func() {
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"{{ .podNamespace }}.svc.{{ .clusterDomain }}", "svc.{{ .clusterDomain }}", "{{ .clusterDomain }}"},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("3")},
					{Name: "edns0"},
				},
			}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, []string{ns.Name}, nil, nil, configv1alpha1.StateReady)

			// Create Pod and verify
			createPodWithOwnerRefAndValidate(corev1.DNSClusterFirst, dnsclass.Spec.DNSPolicy)

			By("Validating PodDNSConfig")
			expectedDNSConfig := &corev1.PodDNSConfig{
				Nameservers: []string{utils.ClusterDNS},
				Searches: []string{
					fmt.Sprintf("%s.svc.%s", pod.Namespace, utils.ClusterDomain),
					fmt.Sprintf("svc.%s", utils.ClusterDomain),
					utils.ClusterDomain,
				},
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: utilptr.To("3")},
					{Name: "edns0"},
				},
			}
			Expect(pod.Spec.DNSConfig).Should(Equal(expectedDNSConfig))
		})

		It("should fail to apply DNSClass to pod due to state is not ready", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{
					"clusterDNS": []string{utils.ClusterDNS},
				},
			}
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{
					"svc.{{ .clusterDomain }}",
					"{{ .podNamespace }}.svc.{{ .clusterDomain }}",
					"{{ .clusterDomain }}"},
			}

			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, []string{ns.Name}, nil, nil, configv1alpha1.StateError)

			createPodAndValidate(corev1.DNSClusterFirst, corev1.DNSClusterFirst)
		})

		It("should not apply DNSClass to pod when nameservers field is missing", func() {
			By("Tampering the configz response")
			utils.MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{},
			}
			dnsconfig := &corev1.PodDNSConfig{}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, []string{ns.Name}, nil, nil, configv1alpha1.StateReady)

			createPodAndValidate(corev1.DNSClusterFirst, corev1.DNSClusterFirst)
		})

		It("should not apply DNSClass to pod when pod namespace is disabled", func() {
			dnsconfig := &corev1.PodDNSConfig{}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, nil, []string{"kube-system", ns.Name}, nil, configv1alpha1.StateReady)

			createPodAndValidate(corev1.DNSClusterFirst, corev1.DNSClusterFirst)
		})

		It("should not apply DNSClass to pod when a namespace is not in allowed list", func() {
			dnsconfig := &corev1.PodDNSConfig{}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, []string{"dummy-namespace"}, nil, nil, configv1alpha1.StateReady)

			createPodAndValidate(corev1.DNSDefault, corev1.DNSDefault)
		})

		It("should not apply DNSClass to pod when a DNSPolicy is not allowed", func() {
			dnsconfig := &corev1.PodDNSConfig{}
			createAndValidateDNSClass(dnsconfig, corev1.DNSNone, nil, nil, []corev1.DNSPolicy{corev1.DNSNone}, configv1alpha1.StateReady)

			createPodAndValidate(corev1.DNSDefault, corev1.DNSDefault)
		})

		It("should apply DNSClass to pod when DNSConfig is nil", func() {
			createAndValidateDNSClass(nil, corev1.DNSClusterFirst, nil, nil, nil, configv1alpha1.StateReady)

			createPodAndValidate(corev1.DNSClusterFirstWithHostNet, corev1.DNSClusterFirst)

			By("Validating Pod annotations")
			Expect(pod.GetAnnotations()[DNSClassName]).Should(Equal(dnsclass.Name))
		})

		It("should apply DNSClass to pod when DNSConfig is nil and DNSPolicy is None", func() {
			createAndValidateDNSClass(nil, corev1.DNSNone, nil, nil, nil, configv1alpha1.StateReady)

			createPodAndValidate(corev1.DNSClusterFirstWithHostNet, corev1.DNSNone)

			By("Validating Pod annotations")
			Expect(pod.GetAnnotations()[DNSClassName]).Should(Equal(dnsclass.Name))
		})
	})

	Context("When calling functions directly", func() {
		It("should fail to decode pod object", func() {
			k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: scheme.Scheme,
			})
			Expect(err).NotTo(HaveOccurred())

			podMutator := &PodMutator{
				Client:  k8sManager.GetClient(),
				Decoder: admission.NewDecoder(k8sManager.GetScheme()),
			}

			podReq := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind:    "Pod",
						Version: "v1",
						Group:   "",
					},
					Name:      "test-pod-dummy",
					Namespace: "test-ns-dummy",
					Object: runtime.RawExtension{
						Raw: nil,
					},
				},
			}
			admissionResponse := podMutator.Handle(context.TODO(), podReq)
			Expect(admissionResponse.AdmissionResponse.Allowed).Should(BeTrue())
		})

		It("should fail to list dnsclass", func() {
			k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: scheme.Scheme,
			})
			Expect(err).NotTo(HaveOccurred())

			podMutator := &PodMutator{
				Client: k8sManager.GetClient(),
			}

			pod := &corev1.Pod{}

			dnsClass, err := podMutator.getDNSClass(context.TODO(), pod)
			Expect(dnsClass).Should(Equal(configv1alpha1.DNSClass{}))
			Expect(err).Should(HaveOccurred())
		})

		It("should fail at template execute due to missing key", func() {
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{
					"svc.{{ .clusterDomain }}",
					"{{ .podNamespace }}.svc.{{ .dnsDomain }}",
					"{{ .clusterName }}"},
			}
			localDNSClass := configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSConfig: dnsconfig,
					DNSPolicy: corev1.DNSNone,
				},
				Status: configv1alpha1.DNSClassStatus{
					State:            configv1alpha1.StateReady,
					DiscoveredFields: &configv1alpha1.DiscoveredFields{},
				},
			}
			localPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-dummy",
					Namespace: "test-ns-dummy",
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
			err := configureDNSForPod(localPod, localDNSClass)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail at template execute", func() {
			dnsconfig := &corev1.PodDNSConfig{
				Searches: []string{"svc.[[ .clusterDomain ]]"},
			}
			localDNSClass := configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSConfig: dnsconfig,
					DNSPolicy: corev1.DNSNone,
				},
				Status: configv1alpha1.DNSClassStatus{
					State:            configv1alpha1.StateReady,
					DiscoveredFields: &configv1alpha1.DiscoveredFields{},
				},
			}
			localPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-dummy",
					Namespace: "test-ns-dummy",
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
			err := configureDNSForPod(localPod, localDNSClass)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail at template parse", func() {
			pod := &corev1.Pod{}
			dnsclass := configv1alpha1.DNSClass{
				Spec: configv1alpha1.DNSClassSpec{
					DNSConfig: &corev1.PodDNSConfig{
						Searches: []string{"{{}}"},
					},
					DNSPolicy: corev1.DNSNone,
				},
				Status: configv1alpha1.DNSClassStatus{
					DiscoveredFields: &configv1alpha1.DiscoveredFields{},
				},
			}

			err := configureDNSForPod(pod, dnsclass)
			Expect(err).Should(HaveOccurred())
		})
	})
})
