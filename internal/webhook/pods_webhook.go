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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
)

const DNSClassName = "kubedns-shepherd.io/dns-class-name"

type PodMutator struct {
	client.Client
	admission.Decoder
}

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

func (p *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	skipMsg := fmt.Sprintf("Skipping DNS configuration for %s/%s", req.Namespace, req.Name)
	pod := &corev1.Pod{}
	err := p.Decode(req, pod)
	if err != nil {
		msg := fmt.Sprintf("Failed to decode the pod. %s", skipMsg)
		logger.Error(err, msg)
		return admission.Allowed(msg)
	}

	podName := pod.GetName()
	if pod.Name == "" {
		podName = fmt.Sprintf("%s... (name pending generation)", pod.GetGenerateName())
	}

	dnsclass, err := getDNSClass(ctx, p.Client, pod.Namespace, pod.Spec.DNSPolicy)
	if err != nil {
		msg := fmt.Sprintf("Failed to detect a DNSClass. %s", skipMsg)
		logger.Info(msg, "error", err)
		return admission.Allowed(msg)
	}

	if dnsclass.Status.State != configv1alpha1.StateReady {
		msg := fmt.Sprintf("DNSClass %s is not available at the moment. %s", dnsclass.Name, skipMsg)
		logger.Info(msg)
		return admission.Allowed(msg)
	}

	err = configureDNSForPod(pod, dnsclass)
	if err != nil {
		msg := fmt.Sprintf("Failed to configure DNS for pod. %s", skipMsg)
		logger.Error(err, msg)
		return admission.Allowed(msg)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		msg := fmt.Sprintf("Failed to marshal pod after DNS configuration. %s", skipMsg)
		logger.Error(err, msg)
		return admission.Allowed(msg)
	}

	logger.Info(fmt.Sprintf("Successfully configured DNS for pod %s/%s with DNSClass %s", pod.Namespace, podName, dnsclass.Name))

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func configureDNSForPod(pod *corev1.Pod, dnsClass configv1alpha1.DNSClass) error {
	if dnsClass.Spec.DNSPolicy == corev1.DNSNone {
		searches := []string{}
		parameterMap := map[string]interface{}{}

		// Add podNamespace if it exists
		if pod.Namespace != "" {
			parameterMap["podNamespace"] = pod.Namespace
		}

		// Add clusterDomain if it exists
		if dnsClass.Status.DiscoveredFields.ClusterDomain != "" {
			parameterMap["clusterDomain"] = dnsClass.Status.DiscoveredFields.ClusterDomain
		}

		// Add clusterName if it exists
		if dnsClass.Status.DiscoveredFields.ClusterName != "" {
			parameterMap["clusterName"] = dnsClass.Status.DiscoveredFields.ClusterName
		}

		// Add dnsDomain if it exists
		if dnsClass.Status.DiscoveredFields.DNSDomain != "" {
			parameterMap["dnsDomain"] = dnsClass.Status.DiscoveredFields.DNSDomain
		}
		if dnsClass.Spec.DNSConfig == nil {
			dnsClass.Spec.DNSConfig = &corev1.PodDNSConfig{}
		}

		for _, search := range dnsClass.Spec.DNSConfig.Searches {
			tmpl, err := template.New("").Option("missingkey=error").Parse(search)
			if err != nil {
				return fmt.Errorf("failed to parse search template: %w", err)
			}

			var tplOutput bytes.Buffer
			// Execute the template with your data
			err = tmpl.Execute(&tplOutput, parameterMap)
			if err != nil {
				return fmt.Errorf("failed to execute DNS template for search '%s': %w", search, err)
			}

			searches = append(searches, tplOutput.String())
		}

		dnsClass.Spec.DNSConfig.Searches = searches
		pod.Spec.DNSConfig = dnsClass.Spec.DNSConfig

		if pod.Spec.DNSConfig.Nameservers == nil && dnsClass.Status.DiscoveredFields.Nameservers == nil {
			return errors.New("no nameservers found")
		}

		if pod.Spec.DNSConfig.Nameservers == nil {
			pod.Spec.DNSConfig.Nameservers = dnsClass.Status.DiscoveredFields.Nameservers
		}
	}

	pod.Spec.DNSPolicy = dnsClass.Spec.DNSPolicy

	updateAnnotation(pod, DNSClassName, dnsClass.Name)
	return nil
}

// UpdateAnnotation updates or adds an annotation to a Kubernetes object
func updateAnnotation(obj client.Object, key, value string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	obj.SetAnnotations(annotations)
}

// GetDNSClass finds the DNSClass for the pod object with its namespace
func getDNSClass(ctx context.Context, c client.Client, podNamespace string, podDNSPolicy corev1.DNSPolicy) (configv1alpha1.DNSClass, error) {
	var dnsClassList configv1alpha1.DNSClassList
	var dnsClass configv1alpha1.DNSClass

	err := c.List(ctx, &dnsClassList)
	if err != nil {
		return configv1alpha1.DNSClass{}, err
	}

	for _, dnsClassItem := range dnsClassList.Items {
		// Skip DNSClass if the namespace is disabled
		if slices.Contains(dnsClassItem.Spec.DisabledNamespaces, podNamespace) {
			continue
		}

		// Ensure the DNS policy is allowed
		if !slices.Contains(dnsClassItem.Spec.AllowedDNSPolicies, podDNSPolicy) {
			continue
		}

		// Select the DNSClass if it's allowed for the namespace or no specific namespaces are defined
		if len(dnsClassItem.Spec.AllowedNamespaces) == 0 || slices.Contains(dnsClassItem.Spec.AllowedNamespaces, podNamespace) {
			return dnsClassItem, nil
		}
	}

	return dnsClass, errors.New("no matching DNSClass found for namespace: " + podNamespace)
}
