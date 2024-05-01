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
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/common"
)

// DNSClassReconciler reconciles a DNSClass object
type DNSClassReconciler struct {
	client.Client

	MaxConcurrentReconcilesForDNSClassReconciler int
}

//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch

// Reconcile reconciles a DNSClass object
func (r *DNSClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling for DNSClass")

	dnsClass := &configv1alpha1.DNSClass{}
	err := r.Get(ctx, req.NamespacedName, dnsClass)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			logger.Info("DNSClass not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DNSClass")
		return ctrl.Result{}, err
	}

	// Mark the resource as not reconciled
	annotations := dnsClass.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	if val, ok := annotations[common.IsReconciled]; !ok || val == "true" {
		annotations[common.IsReconciled] = "false"
		dnsClass.SetAnnotations(annotations)
		if err := r.Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to add annotations to DNSClass")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: common.ReconcilePeriod}, nil
	}

	// Set the status as Unknown when no status are available
	if dnsClass.Status.Conditions == nil || len(dnsClass.Status.Conditions) == 0 {
		meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeAvailable, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to update DNSClass to add the status")
			return ctrl.Result{}, err
		}
	}

	// Fill discovered fields in DNSClass object
	if dnsClass.Spec.DNSPolicy == corev1.DNSNone {
		undiscoveredField := []string{}
		discoveredFields := &configv1alpha1.DiscoveredFields{}
		if dnsClass.Spec.DiscoveredFields != nil {
			discoveredFields = dnsClass.Spec.DiscoveredFields
		}
		discoveredFields.Nameservers, err = r.getNameservers(ctx)
		if err != nil {
			undiscoveredField = append(undiscoveredField, "nameservers")
			logger.Info("Failed to discover nameservers", "error", err)
		}

		discoveredFields.ClusterDomain, err = r.getClusterDomain(ctx)
		if err != nil {
			undiscoveredField = append(undiscoveredField, "clusterDomain")
			logger.Info("Failed to discover clusterDomain", "error", err)
		}

		discoveredFields.ClusterName, err = r.getClusterName(ctx)
		if err != nil {
			undiscoveredField = append(undiscoveredField, "clusterName")
			logger.Info("Failed to discover clusterName", "error", err)
		}

		discoveredFields.DNSDomain, err = r.getDNSDomain(ctx)
		if err != nil {
			undiscoveredField = append(undiscoveredField, "dnsDomain")
			logger.Info("Failed to discover dnsDomain", "error", err)
		}

		// Update status if any discovery failing
		if undiscoveredField != nil {
			meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeAvailable,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to add some discovered fields; %v. Ignore if the parameters are not used.", undiscoveredField)})
			if err = r.Status().Update(ctx, dnsClass); err != nil {
				logger.Error(err, "Failed to update DNSClass to add the status")
				return ctrl.Result{}, err
			}
		}

		if !reflect.DeepEqual(discoveredFields, dnsClass.Spec.DiscoveredFields) {
			dnsClass.Spec.DiscoveredFields = discoveredFields

			if err := r.Update(ctx, dnsClass); err != nil {
				logger.Error(err, "Failed to update DNSClass to add discovered fields")
				return ctrl.Result{}, err
			}

			return ctrl.Result{Requeue: true, RequeueAfter: common.ReconcilePeriod}, nil
		}
	}

	// Mark the resource as reconciled
	annotations[common.IsReconciled] = "true"
	dnsClass.SetAnnotations(annotations)

	if err := r.Update(ctx, dnsClass); err != nil {
		logger.Error(err, "Failed to update DNSClass to add annotations")
		return ctrl.Result{}, err
	}

	// Update status
	meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeAvailable,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: "DNSClass reconciled."})

	if err := r.Status().Update(ctx, dnsClass); err != nil {
		logger.Error(err, "Failed to update DNSClass to add the status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DNSClassReconciler) getDNSDomain(ctx context.Context) (string, error) {
	data, err := common.GetConfigMapData(ctx, r.Client, "kubeadm-config", "ClusterConfiguration")
	if err != nil {
		return "", err
	}

	networking, ok := data["networking"].(map[interface{}]interface{})
	if !ok {
		return "", errors.New("networking field is not a map[string]interface{}")
	}

	dnsDomain, ok := networking["dnsDomain"].(string)
	if !ok {
		return "", errors.New("dnsDomain field is not a string")
	}

	return dnsDomain, nil
}

func (r *DNSClassReconciler) getClusterName(ctx context.Context) (string, error) {
	data, err := common.GetConfigMapData(ctx, r.Client, "kubeadm-config", "ClusterConfiguration")
	if err != nil {
		return "", err
	}

	clusterName, ok := data["clusterName"].(string)
	if !ok {
		return "", errors.New("clusterName field is not a string")
	}

	return clusterName, nil
}

func (r *DNSClassReconciler) getClusterDomain(ctx context.Context) (string, error) {
	data, err := common.GetConfigMapData(ctx, r.Client, "kubelet-config", "kubelet")
	if err != nil {
		return "", err
	}

	clusterDomain, ok := data["clusterDomain"].(string)
	if !ok {
		return "", errors.New("clusterDNS field is not a string array")
	}

	return clusterDomain, nil
}

func (r *DNSClassReconciler) getNameservers(ctx context.Context) ([]string, error) {
	data, err := common.GetConfigMapData(ctx, r.Client, "kubelet-config", "kubelet")
	if err != nil {
		return nil, err
	}

	clusterDNSInterfaceSlice, ok := data["clusterDNS"].([]interface{})
	if !ok {
		return nil, errors.New("clusterDNS field is not a string array")
	}

	// Convert interface slice to string slice
	clusterDNSStringSlice := make([]string, len(clusterDNSInterfaceSlice))
	for i, v := range clusterDNSInterfaceSlice {
		clusterDNSStringSlice[i] = v.(string)
	}

	return clusterDNSStringSlice, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.DNSClass{}).
		WithEventFilter(&common.DnsClassPredicate{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconcilesForDNSClassReconciler}).
		Complete(r)
}
