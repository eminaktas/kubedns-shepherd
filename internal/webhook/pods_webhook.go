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

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

func (p *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)
	pod := &corev1.Pod{}
	err := p.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	dnsConfigurationDisabled := common.IsDNSConfigurable(pod)
	if dnsConfigurationDisabled {
		msg := fmt.Sprintf("DNS configuration has been disabled with %s=true annotation", common.DNSConfigurationDisabled)
		logger.Info(msg)
		return admission.Allowed(msg)
	}

	var dnsClass configv1alpha1.DNSClass
	dnsClass, err = common.GetDNSClass(ctx, p.Client, pod.Namespace)
	if err != nil {
		msg := "Failed to find a DNSClass"
		logger.Info(msg, "error", err)
		return admission.Allowed(msg)
	}

	alreadyDNSConfigured := common.IsDNSConfigured(pod, dnsClass.Spec.DNSConfig)
	if alreadyDNSConfigured {
		msg := "DNS configuration has been configured, no need to reconcile"
		logger.Info("DNS configuration has been configured, no need to reconcile")
		return admission.Allowed(msg)
	}

	// Configure Pod Object
	err = p.configureDNSForPod(pod, dnsClass)
	if err != nil {
		msg := fmt.Sprintf("Failed to configure %s/%s pod", pod.Namespace, pod.Name)
		logger.Error(err, msg)
		return admission.Allowed(msg)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info(fmt.Sprintf("DNSConfig configured for %s/%s with %s DNSClass", pod.Namespace, pod.Name, dnsClass.Name))

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (r *PodMutator) configureDNSForPod(pod *corev1.Pod, dnsClass configv1alpha1.DNSClass) error {
	if err := common.SetDNSConfig(pod, dnsClass.Spec.DNSConfig); err != nil {
		return err
	}

	if err := common.SetDNSPolicyTo(pod, corev1.DNSNone); err != nil {
		return err
	}

	if err := common.UpdateAnnotation(pod, common.DNSConfigured, "true"); err != nil {
		return err
	}

	if err := common.UpdateAnnotation(pod, common.DNSClassName, dnsClass.Name); err != nil {
		return err
	}

	return nil
}
