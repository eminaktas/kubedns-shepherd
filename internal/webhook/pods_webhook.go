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
	"net/http"
	"reflect"
	"slices"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/controller"
)

const DNSClassName = "kubedns-shepherd.io/dns-class-name"

type PodMutator struct {
	client.Client
	*admission.Decoder
}

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

func (p *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)
	pod := &corev1.Pod{}
	err := p.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	podName := pod.GetName()
	if pod.Name == "" {
		podName = fmt.Sprintf("%s... (name pending generation)", pod.GetGenerateName())
	}

	var dnsClass configv1alpha1.DNSClass
	dnsClass, err = getDNSClass(ctx, p.Client, pod.Namespace)
	if err != nil {
		msg := fmt.Sprintf("Failed to detect a DNSClass for %s/%s. Skipping update for this resource", pod.Namespace, podName)
		logger.Info(msg, "error", err)
		return admission.Allowed(msg)
	}

	// Check if dnsClass is available
	notAvailable := false
	annotations := dnsClass.GetAnnotations()
	if annotations == nil {
		notAvailable = true
	}
	if val, ok := annotations[controller.IsReconciled]; !ok || val == "false" {
		notAvailable = true
	}
	if notAvailable {
		msg := fmt.Sprintf("Found DNSClass is not available for %s/%s. Skipping update for this resource", pod.Namespace, podName)
		logger.Info(msg, "error", err)
		return admission.Allowed(msg)
	}

	// Configure Pod Object
	err = configureDNSForPod(pod, dnsClass)
	if err != nil {
		msg := fmt.Sprintf("Failed to configure for %s/%s. Skipping update for this resource", pod.Namespace, podName)
		logger.Error(err, msg)
		return admission.Allowed(msg)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info(fmt.Sprintf("DNSConfig configured for %s/%s with %s DNSClass", pod.Namespace, podName, dnsClass.Name))

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func configureDNSForPod(pod *corev1.Pod, dnsClass configv1alpha1.DNSClass) error {
	if dnsClass.Spec.DNSPolicy == corev1.DNSNone {
		// Update DNSClass for dynamic parameters in searches
		searches := []string{}
		parameterMap := map[string]interface{}{
			"podNamespace":  pod.Namespace,
			"clusterDomain": dnsClass.Status.DiscoveredFields.ClusterDomain,
			"clusterName":   dnsClass.Status.DiscoveredFields.ClusterName,
			"dnsDomain":     dnsClass.Status.DiscoveredFields.DNSDomain,
		}
		for _, search := range dnsClass.Spec.DNSConfig.Searches {
			tmpl, err := template.New("").Parse(search)
			if err != nil {
				return err
			}

			var tplOutput bytes.Buffer
			// Execute the template with your data
			err = tmpl.Execute(&tplOutput, parameterMap)
			if err != nil {
				return err
			}

			searches = append(searches, tplOutput.String())
		}
		dnsClass.Spec.DNSConfig.Searches = searches

		pod.Spec.DNSConfig = dnsClass.Spec.DNSConfig

		if pod.Spec.DNSConfig.Nameservers == nil {
			pod.Spec.DNSConfig.Nameservers = dnsClass.Status.DiscoveredFields.Nameservers
		}
	}

	// Set DNSPolicy to None
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
func getDNSClass(ctx context.Context, c client.Client, podNamespace string) (configv1alpha1.DNSClass, error) {
	var (
		dnsClass     configv1alpha1.DNSClass
		dnsClassList configv1alpha1.DNSClassList
	)

	err := c.List(ctx, &dnsClassList)
	if err != nil {
		return configv1alpha1.DNSClass{}, err
	}

	for _, dnsClassItem := range dnsClassList.Items {
		if slices.Contains(dnsClassItem.Spec.DisabledNamespaces, podNamespace) {
			continue
		}
		if slices.Contains(dnsClassItem.Spec.AllowedNamespaces, podNamespace) {
			dnsClass = dnsClassItem
			break // Exit the loop once a DNSClass is found
		}
		if dnsClassItem.Spec.AllowedNamespaces == nil {
			dnsClass = dnsClassItem
		}
	}

	if reflect.DeepEqual(dnsClass, configv1alpha1.DNSClass{}) {
		return configv1alpha1.DNSClass{}, errors.New("no matching DNSClass found for namespace: " + podNamespace)
	}
	return dnsClass, nil
}
