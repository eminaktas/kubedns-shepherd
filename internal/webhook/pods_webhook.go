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
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/common"
)

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
		podName = pod.GetGenerateName() + "*"
	}

	var dnsClass configv1alpha1.DNSClass
	dnsClass, err = common.GetDNSClass(ctx, p.Client, pod.Namespace)
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
	if val, ok := annotations[common.IsReconciled]; !ok || val == "false" {
		notAvailable = true
	}
	if notAvailable {
		msg := fmt.Sprintf("Found DNSClass is not available for %s/%s. Skipping update for this resource", pod.Namespace, podName)
		logger.Info(msg, "error", err)
		return admission.Allowed(msg)
	}

	// Configure Pod Object
	err = p.configureDNSForPod(pod, dnsClass)
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

func (r *PodMutator) configureDNSForPod(pod *corev1.Pod, dnsClass configv1alpha1.DNSClass) error {
	// Set DNSConfig
	if dnsClass.Spec.DNSPolicy == corev1.DNSNone {
		pod.Spec.DNSConfig = dnsClass.Spec.DNSConfig
	}

	// Set DNSPolicy to None
	pod.Spec.DNSPolicy = dnsClass.Spec.DNSPolicy

	if err := common.UpdateAnnotation(pod, common.DNSClassName, dnsClass.Name); err != nil {
		return err
	}

	return nil
}
