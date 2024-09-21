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
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Logger for DNSClass package
var dnsclasslog = logf.Log.WithName("dnsclass-resource")

// Predefined constants for DNS policies and template keys
var (
	ValidDNSPolicies   = []corev1.DNSPolicy{corev1.DNSDefault, corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet, corev1.DNSNone}
	DefaultDNSPolicies = []corev1.DNSPolicy{corev1.DNSClusterFirst, corev1.DNSClusterFirstWithHostNet, corev1.DNSNone}
	ValidTemplateKeys  = []string{"podNamespace", "clusterDomain", "dnsDomain", "clusterName"}
)

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *DNSClass) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithDefaulter(&DNSClassCustomDefaulter{}).
		WithValidator(&DNSClassCustomValidator{}).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-config-kubedns-shepherd-io-v1alpha1-dnsclass,mutating=true,failurePolicy=fail,sideEffects=None,groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=create;update,versions=v1alpha1,name=mdnsclass.kb.io,admissionReviewVersions=v1

type DNSClassCustomDefaulter struct {
}

var _ webhook.CustomDefaulter = &DNSClassCustomDefaulter{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *DNSClassCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	dnsclass := obj.(*DNSClass)
	dnsclasslog.Info("default", "name", dnsclass.Name)

	if dnsclass.Spec.AllowedDNSPolicies == nil {
		dnsclass.Spec.AllowedDNSPolicies = DefaultDNSPolicies
	}
	if dnsclass.Spec.DNSPolicy == "" {
		dnsclass.Spec.DNSPolicy = corev1.DNSNone
	}

	return nil
}

//+kubebuilder:webhook:path=/validate-config-kubedns-shepherd-io-v1alpha1-dnsclass,mutating=false,failurePolicy=fail,sideEffects=None,groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=create;update,versions=v1alpha1,name=vdnsclass.kb.io,admissionReviewVersions=v1

type DNSClassCustomValidator struct {
}

var _ webhook.CustomValidator = &DNSClassCustomValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DNSClassCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dnsclass := obj.(*DNSClass)
	dnsclasslog.Info("validate create", "name", dnsclass.Name)
	return r.validate(dnsclass)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DNSClassCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dnsclass := newObj.(*DNSClass)
	dnsclasslog.Info("validate update", "name", dnsclass.Name)
	return r.validate(dnsclass)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DNSClassCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation required on delete for now
	return nil, nil
}

func (r *DNSClassCustomValidator) validate(dnsclass *DNSClass) (admission.Warnings, error) {
	var allErrs field.ErrorList
	// Check if all DNS policies in AllowedDNSPolicies are valid
	for _, dnsPolicy := range dnsclass.Spec.AllowedDNSPolicies {
		if !slices.Contains(ValidDNSPolicies, dnsPolicy) {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("dnsPolicy"), dnsclass.Name, fmt.Sprintf("invalid DNS policy: %s. Allowed policies: %v", dnsPolicy, ValidDNSPolicies)))
		}
	}

	// Ensure the DNSPolicy itself is valid
	if !slices.Contains(ValidDNSPolicies, dnsclass.Spec.DNSPolicy) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("dnsPolicy"), dnsclass.Name, fmt.Sprintf("invalid DNSPolicy: %s. Allowed policies: %v", dnsclass.Spec.DNSPolicy, ValidDNSPolicies)))
	}

	// Ensure that DNSConfig is only set if DNSPolicy is None
	if dnsclass.Spec.DNSPolicy != corev1.DNSNone && dnsclass.Spec.DNSConfig != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("dnsPolicy"), dnsclass.Name, fmt.Sprintf("DNSConfig cannot be set when DNSPolicy is %s", dnsclass.Spec.DNSPolicy)))
	}

	// Validate template keys in DNSConfig.Searches
	for _, key := range dnsclass.ExtractTemplateKeysRegex() {
		if !slices.Contains(ValidTemplateKeys, key) {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "dnsConfig").Child("searches"), dnsclass.Name, fmt.Sprintf("invalid template key: %s in searches. Allowed keys: %v", key, ValidTemplateKeys)))
		}
	}
	if len(allErrs) == 0 {
		return nil, nil
	}
	dnsclasslog.Error(errors.New(allErrs.ToAggregate().Error()), "validation failed")

	return nil, apierrors.NewInvalid(
		dnsclass.GroupVersionKind().GroupKind(),
		dnsclass.Name,
		allErrs,
	)
}

// Extract the keys using a regular expression
func (r *DNSClass) ExtractTemplateKeysRegex() []string {
	// Regular expression to match {{ .key }} patterns
	re := regexp.MustCompile(`{{\s*\.([a-zA-Z0-9_]+)\s*}}`)

	keys := []string{}
	if r.Spec.DNSConfig != nil {
		for _, search := range r.Spec.DNSConfig.Searches {
			// Find all matches in the template string
			matches := re.FindAllStringSubmatch(search, -1)
			// Extract the keys from the matches
			for _, match := range matches {
				if len(match) > 1 && !slices.Contains(keys, match[1]) {
					keys = append(keys, match[1]) // match[1] contains the key (e.g., "clusterDomain")
				}
			}
		}
	}
	return keys
}
