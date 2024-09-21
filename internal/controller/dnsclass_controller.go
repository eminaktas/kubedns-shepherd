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
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
)

// Workload used to define objects
type Workload struct {
	Name      string
	Namespace string
	Kind      string
}

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

	dnsclass := &configv1alpha1.DNSClass{}
	err := r.Get(ctx, req.NamespacedName, dnsclass)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DNSClass not found. Assuming it has been deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DNSClass")
		return ctrl.Result{}, err
	}

	// Set status to Init if it is not set
	if dnsclass.Status.State == "" {
		dnsclass.Status.State = configv1alpha1.StateInit
	}

	statusMessage := "Initialization in progress"
	defer func() {
		if err := r.updateStatus(ctx, err, statusMessage, dnsclass); err != nil {
			logger.Error(err, "Failed to update DNSClass status")
		}
	}()

	if dnsclass.Spec.DNSPolicy == corev1.DNSNone {
		discoveredFields := dnsclass.Status.DiscoveredFields
		if discoveredFields == nil {
			discoveredFields = &configv1alpha1.DiscoveredFields{}
		}

		undiscoveredFields, message := r.discoverFields(ctx, discoveredFields)
		statusMessage = message
		if len(undiscoveredFields) > 0 {
			for _, key := range dnsclass.ExtractTemplateKeysRegex() {
				if !slices.Contains(undiscoveredFields, key) {
					err = errors.Errorf("failed to discover template keys: %v", strings.Join(undiscoveredFields, ", "))
					return ctrl.Result{}, err
				}
			}
		}

		if !reflect.DeepEqual(discoveredFields, dnsclass.Status.DiscoveredFields) {
			dnsclass.Status.DiscoveredFields = discoveredFields
		}
	}

	dnsclass.Status.State = configv1alpha1.StateReady
	statusMessage = "Ready"

	logger.Info("Reconciling completed for DNSClass")

	return ctrl.Result{}, nil
}

func (r *DNSClassReconciler) discoverFields(ctx context.Context, discoveredFields *configv1alpha1.DiscoveredFields) ([]string, string) {
	var err error
	undiscoveredFields := []string{}
	errorMessages := []string{}

	if discoveredFields.Nameservers, err = r.getNameservers(ctx); err != nil {
		undiscoveredFields = append(undiscoveredFields, "nameservers")
		errorMessages = append(errorMessages, fmt.Sprintf("Nameservers: %v", err))
	}

	if discoveredFields.ClusterDomain, err = r.getClusterDomain(ctx); err != nil {
		undiscoveredFields = append(undiscoveredFields, "clusterDomain")
		errorMessages = append(errorMessages, fmt.Sprintf("ClusterDomain: %v", err))
	}

	if discoveredFields.ClusterName, err = r.getClusterName(ctx); err != nil {
		undiscoveredFields = append(undiscoveredFields, "clusterName")
		errorMessages = append(errorMessages, fmt.Sprintf("ClusterName: %v", err))
	}

	if discoveredFields.DNSDomain, err = r.getDNSDomain(ctx); err != nil {
		undiscoveredFields = append(undiscoveredFields, "dnsDomain")
		errorMessages = append(errorMessages, fmt.Sprintf("DNSDomain: %v", err))
	}

	// Combine error messages into one if there were any errors
	if len(errorMessages) > 0 {
		return undiscoveredFields, fmt.Sprintf("discovery failed for the following fields: %s", strings.Join(errorMessages, "; "))
	}

	return undiscoveredFields, ""
}

func (r *DNSClassReconciler) getDNSDomain(ctx context.Context) (string, error) {
	data, err := r.getConfigMapData(ctx, "kubeadm-config", "ClusterConfiguration")
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
	data, err := r.getConfigMapData(ctx, "kubeadm-config", "ClusterConfiguration")
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
	data, err := r.getConfigMapData(ctx, "kubelet-config", "kubelet")
	if err != nil {
		return "", err
	}

	clusterDomain, ok := data["clusterDomain"].(string)
	if !ok {
		return "", errors.New("clusterDNS field is not a string")
	}

	return clusterDomain, nil
}

func (r *DNSClassReconciler) getNameservers(ctx context.Context) ([]string, error) {
	data, err := r.getConfigMapData(ctx, "kubelet-config", "kubelet")
	if err != nil {
		return nil, err
	}

	clusterDNSInterfaceSlice, ok := data["clusterDNS"].([]interface{})
	if !ok {
		return nil, errors.New("clusterDNS field is not an interface array")
	}

	// Convert interface slice to string slice
	clusterDNSStringSlice := make([]string, len(clusterDNSInterfaceSlice))
	for i, v := range clusterDNSInterfaceSlice {
		clusterDNSStringSlice[i] = v.(string)
	}

	return clusterDNSStringSlice, nil
}

// GetConfigMapData gets specified data in given ConfigMap from kube-system namespace
func (r *DNSClassReconciler) getConfigMapData(ctx context.Context, configMapName, keyName string) (map[string]interface{}, error) {
	cmNamespacedName := types.NamespacedName{Name: configMapName, Namespace: "kube-system"}
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, cmNamespacedName, cm); err != nil {
		return nil, err
	}

	if data, ok := cm.Data[keyName]; ok {
		config := make(map[string]interface{})
		if err := yaml.Unmarshal([]byte(data), &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal %s config: %w", keyName, err)
		}

		return config, nil
	}
	return nil, fmt.Errorf("%s key not found in %s data", keyName, configMapName)
}

func (r *DNSClassReconciler) updateStatus(ctx context.Context, rErr error, message string, dnsclass *configv1alpha1.DNSClass) error {
	condition := metav1.Condition{
		Type:               configv1alpha1.StateInit,
		Status:             metav1.ConditionUnknown,
		Message:            message,
		Reason:             "ReconcileStart",
		LastTransitionTime: metav1.NewTime(time.Now()),
	}

	if rErr != nil {
		condition = metav1.Condition{
			Type:               configv1alpha1.StateError,
			Status:             metav1.ConditionTrue,
			Message:            rErr.Error(),
			Reason:             "ReconcileError",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}

		dnsclass.Status.State = configv1alpha1.StateError
		meta.SetStatusCondition(&dnsclass.Status.Conditions, condition)

		return r.writeStatus(ctx, dnsclass)
	}

	if dnsclass.Status.State == configv1alpha1.StateReady {
		condition = metav1.Condition{
			Type:               configv1alpha1.StateReady,
			Status:             metav1.ConditionTrue,
			Message:            message,
			Reason:             "ReconcileCompleted",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	}

	meta.SetStatusCondition(&dnsclass.Status.Conditions, condition)

	return r.writeStatus(ctx, dnsclass)
}

func (r *DNSClassReconciler) writeStatus(ctx context.Context, dnsclass *configv1alpha1.DNSClass) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		dc := &configv1alpha1.DNSClass{}

		err := r.Get(ctx, types.NamespacedName{Name: dnsclass.Name, Namespace: dnsclass.Namespace}, dc)
		if err != nil {
			return err
		}

		dc.Status = dnsclass.Status
		return r.Status().Update(ctx, dc)
	})

	if apierrors.IsNotFound(err) {
		return nil
	}

	return errors.Wrap(err, "update status")
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.DNSClass{}).
		WithEventFilter(&dnsClassPredicate{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconcilesForDNSClassReconciler}).
		Complete(r)
}

// DnsClassPredicate is a predicate for DNSClass objects
type dnsClassPredicate struct {
	predicate.Funcs
}

// Create checks if a DNSClass object is marked as reconciled
func (*dnsClassPredicate) Create(e event.CreateEvent) bool {
	return true
}

// Update checks if a DNSClass object is updated
func (*dnsClassPredicate) Update(e event.UpdateEvent) bool {
	// If resources updated, we need to validate for DNS Configuration
	return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
}

// Delete always returns false for DNSClass deletion events
func (*dnsClassPredicate) Delete(e event.DeleteEvent) bool {
	return false
}
