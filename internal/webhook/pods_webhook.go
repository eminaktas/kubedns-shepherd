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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
)

const DNSClassName = "kubedns-shepherd.io/dns-class-name"

var (
	ErrPodDecode            = errors.New("failed to decode pod object")
	ErrNoDNSClass           = errors.New("no matching DNSClass found")
	ErrNotAvailableDNSClass = errors.New("%s DNSClass not currently available")
	ErrDNSConfig            = errors.New("failed to configure %s DNSClass")
	ErrMarshal              = errors.New("json marshal failed")
)

type PodMutator struct {
	client.Client
	admission.Decoder
	record.EventRecorder
}

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kubedns-shepherd.io,admissionReviewVersions=v1

func (p *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	err := p.Decode(req, pod)
	if err != nil {
		logger.Error(err, ErrPodDecode.Error())
		p.Event(pod, corev1.EventTypeWarning, "FailedDNSClassMutation", ErrPodDecode.Error())
		return admission.Allowed(ErrPodDecode.Error())
	}

	// Create a deep copy of the pod to safely modify its fields for events reference
	copyPod := pod.DeepCopy()

	// Set the pod name in the copy if it hasn't been generated yet
	// This ensures a name is available for events even if the original pod is pending name generation
	if pod.GetName() == "" {
		copyPod.SetName(fmt.Sprintf("%s<unknown>", pod.GetGenerateName()))
	}

	dnsclass, err := p.getDNSClass(ctx, pod)
	if err != nil {
		logger.Info(err.Error())
		p.Event(copyPod, corev1.EventTypeNormal, "SkippedDNSClassMutation", err.Error())
		return admission.Allowed(err.Error())
	}

	dnsclassName := dnsclass.GetName()

	if dnsclass.Status.State != configv1alpha1.StateReady {
		logger.Info(ErrNotAvailableDNSClass.Error(), "dnsclass", dnsclassName)
		p.Eventf(copyPod, corev1.EventTypeWarning, "FailedDNSClassMutation", ErrNotAvailableDNSClass.Error(), dnsclassName)
		return admission.Allowed(fmt.Sprintf(ErrNotAvailableDNSClass.Error(), dnsclassName))
	}

	err = configureDNSForPod(pod, dnsclass)
	if err != nil {
		logger.Error(err, ErrDNSConfig.Error())
		p.Eventf(copyPod, corev1.EventTypeWarning, "FailedDNSClassMutation", ErrDNSConfig.Error(), dnsclassName)
		return admission.Allowed(fmt.Sprintf(ErrDNSConfig.Error(), dnsclassName))
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, ErrMarshal.Error())
		p.Event(copyPod, corev1.EventTypeWarning, "FailedDNSClassMutation", ErrMarshal.Error())
		return admission.Allowed(ErrMarshal.Error())
	}

	logger.Info(fmt.Sprintf("Successfully configured DNS for pod %s/%s with DNSClass %s", pod.GetNamespace(), pod.GetName(), dnsclassName))
	p.Event(copyPod, corev1.EventTypeNormal, "SuccessfulDNSClassMutation", fmt.Sprintf("DNS configuration successfully applied from DNSClass resource '%s'", dnsclassName))

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// GetDNSClass finds the DNSClass for the pod object with its namespace
func (p *PodMutator) getDNSClass(ctx context.Context, pod *corev1.Pod) (configv1alpha1.DNSClass, error) {
	var dnsClassList configv1alpha1.DNSClassList
	var dnsClass configv1alpha1.DNSClass

	err := p.List(ctx, &dnsClassList)
	if err != nil {
		return configv1alpha1.DNSClass{}, err
	}

	podNamespace := pod.GetNamespace()

	for _, dnsClassItem := range dnsClassList.Items {
		// Skip DNSClass if the namespace is disabled
		if slices.Contains(dnsClassItem.Spec.DisabledNamespaces, podNamespace) {
			continue
		}

		// Ensure the DNS policy is allowed
		if !slices.Contains(dnsClassItem.Spec.AllowedDNSPolicies, pod.Spec.DNSPolicy) {
			continue
		}

		// Select the DNSClass if it's allowed for the namespace or no specific namespaces are defined
		if len(dnsClassItem.Spec.AllowedNamespaces) == 0 || slices.Contains(dnsClassItem.Spec.AllowedNamespaces, podNamespace) {
			return dnsClassItem, nil
		}
	}

	return dnsClass, ErrNoDNSClass
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
