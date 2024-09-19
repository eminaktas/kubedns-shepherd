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
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var dnsclasslog = logf.Log.WithName("dnsclass-resource")

var validDNSPolicies = []corev1.DNSPolicy{corev1.DNSDefault, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet, corev1.DNSNone}
var defaultDNSPolicies = []corev1.DNSPolicy{corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet, corev1.DNSNone}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *DNSClass) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-config-kubedns-shepherd-io-v1alpha1-dnsclass,mutating=true,failurePolicy=fail,sideEffects=None,groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=create;update,versions=v1alpha1,name=mdnsclass.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &DNSClass{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *DNSClass) Default() {
	dnsclasslog.Info("default", "name", r.Name)

	if r.Spec.AllowedDNSPolicies == nil {
		r.Spec.AllowedDNSPolicies = defaultDNSPolicies
	}
	if r.Spec.DNSPolicy == "" {
		r.Spec.DNSPolicy = corev1.DNSNone
	}
}

//+kubebuilder:webhook:path=/validate-config-kubedns-shepherd-io-v1alpha1-dnsclass,mutating=false,failurePolicy=fail,sideEffects=None,groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=create;update,versions=v1alpha1,name=vdnsclass.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &DNSClass{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DNSClass) ValidateCreate() (admission.Warnings, error) {
	dnsclasslog.Info("validate create", "name", r.Name)
	return r.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DNSClass) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	dnsclasslog.Info("validate update", "name", r.Name)
	return r.validate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DNSClass) ValidateDelete() (admission.Warnings, error) {
	// Not used
	return nil, nil
}

func (r *DNSClass) validate() (admission.Warnings, error) {
	// Controls allowedDNSPolicies are valid
	for _, dnsPolicy := range r.Spec.AllowedDNSPolicies {
		if !slices.Contains(validDNSPolicies, dnsPolicy) {
			return nil, fmt.Errorf("%s is not allowed for allowedDNSPolicies. Allowed DNS Policies: %v", dnsPolicy, validDNSPolicies)
		}
	}

	// Controls if the DNSPolicy is valid
	if !slices.Contains(validDNSPolicies, r.Spec.DNSPolicy) {
		return nil, fmt.Errorf("%s is not allowed for dnsPolicy. Allowed DNS Policies: %v", r.Spec.DNSPolicy, validDNSPolicies)
	}

	// Controls if DNSConfig is only set with DNSPolicy `None`
	if r.Spec.DNSPolicy != corev1.DNSNone && r.Spec.DNSConfig != nil {
		return nil, fmt.Errorf("%s is not allowed to define when dnsConfig is defined", r.Spec.DNSPolicy)
	}

	return nil, nil
}
