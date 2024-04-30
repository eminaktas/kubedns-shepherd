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
	"reflect"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses/finalizers,verbs=update
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

	// Add default nameservers if not defined in object
	if dnsClass.Spec.DNSPolicy == corev1.DNSNone {
		if dnsClass.Spec.DNSConfig.Nameservers == nil {
			dnsClass.Spec.DNSConfig.Nameservers, err = r.getNameservers(ctx)
			if err != nil {
				logger.Error(err, "Failed to set nameservers to DNSClass")
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, dnsClass); err != nil {
				logger.Error(err, "Failed to update DNSClass to add nameservers")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true, RequeueAfter: common.ReconcilePeriod}, nil
		}
	}

	// Set the status as Unknown when no status are available
	if dnsClass.Status.Conditions == nil || len(dnsClass.Status.Conditions) == 0 {
		meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeAvailable, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to update DNSClass to add the status")
			return ctrl.Result{}, err
		}
	}

	if dnsClass.Spec.EnablePodRestart {
		// Manage finalizers
		if !controllerutil.ContainsFinalizer(dnsClass, common.FinalizerString) {
			logger.Info("Adding Finalizer")
			if ok := controllerutil.AddFinalizer(dnsClass, common.FinalizerString); !ok {
				logger.Error(err, "Failed to add finalizer to DNSClass")
				return ctrl.Result{Requeue: true}, nil
			}

			if err = r.Update(ctx, dnsClass); err != nil {
				logger.Error(err, "Failed to update DNSClass to add finalizer")
				return ctrl.Result{}, err
			}

			return ctrl.Result{Requeue: true, RequeueAfter: common.ReconcilePeriod}, nil
		}

		// Perform finalizer operations before deletion
		if !dnsClass.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(dnsClass, common.FinalizerString) {
				logger.Info("Performing Finalizer Operations before deleting DNSClass")

				// Add "Downgrade" status
				if !meta.IsStatusConditionPresentAndEqual(dnsClass.Status.Conditions, common.TypeDegraded, metav1.ConditionUnknown) {
					meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeDegraded,
						Status: metav1.ConditionUnknown, Reason: "Finalizing",
						Message: "Performing finalizer operations for DNSClass"})

					if err := r.Status().Update(ctx, dnsClass); err != nil {
						logger.Error(err, "Failed to update DNSClass to add the status")
						return ctrl.Result{}, err
					}
					return ctrl.Result{Requeue: true, RequeueAfter: common.ReconcilePeriod}, nil
				}

				// Revert the DNS Configuration
				err := r.triggerRestart(ctx, dnsClass)
				if err != nil {
					logger.Error(err, "Failed to restart pods to revert the DNS Configuration for matching workloads")
					return ctrl.Result{}, err
				}

				meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeDegraded,
					Status: metav1.ConditionTrue, Reason: "Finalizing",
					Message: "Finalizer operations were successfully accomplished"})

				if err := r.Status().Update(ctx, dnsClass); err != nil {
					logger.Error(err, "Failed to update DNSClass to add the status")
					return ctrl.Result{}, err
				}

				logger.Info("Removing Finalizer after successfully perform the operations")
				if ok := controllerutil.RemoveFinalizer(dnsClass, common.FinalizerString); !ok {
					logger.Error(err, "Failed to remove finalizer from DNSClass")
					return ctrl.Result{Requeue: true}, nil
				}

				if err := r.Update(ctx, dnsClass); err != nil {
					logger.Error(err, "Failed to update DNSClass to remove finalizer")
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{}, nil
		}
	}
	// Mark the resource as reconciled
	annotations[common.IsReconciled] = "true"
	dnsClass.SetAnnotations(annotations)

	if err := r.Update(ctx, dnsClass); err != nil {
		logger.Error(err, "Failed to update DNSClass to add annotations")
		return ctrl.Result{}, err
	}

	if dnsClass.Spec.EnablePodRestart {
		// Restart workloads
		err = r.triggerRestart(ctx, dnsClass)
		if err != nil {
			logger.Error(err, "Failed to restart pods to apply the DNS Configuration for matching workloads")
			return ctrl.Result{}, err
		}
	}

	// Update status
	meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: common.TypeAvailable,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: "DNSClass reconciled; workloads restarted for DNS configuration update"})

	if err := r.Status().Update(ctx, dnsClass); err != nil {
		logger.Error(err, "Failed to update DNSClass to add the status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// triggerRestart adds an annotations to restart pods, this way webhook will take over to add the dns configurations
func (r *DNSClassReconciler) triggerRestart(ctx context.Context, dnsClass *configv1alpha1.DNSClass) error {
	logger := log.FromContext(ctx)
	workloads, err := common.ListWorkloadObjects(ctx, r.Client, dnsClass)
	if err != nil {
		return err
	}
	for _, workload := range workloads {
		// Pods are not allowed to be restarted or manupulated once deployed in cluster.
		if workload.Kind == common.PodStr {
			logger.Info("Pods cannot be restarted because updating pod resources is not supported")
			continue
		}

		object, err := common.GetObjectFromKindString(workload.Kind)
		if err != nil {
			return err
		}

		if err = r.Get(ctx, types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}, object); err != nil {
			return err
		}

		currentTime := time.Now().Format("2006-01-02T15:04:05Z")
		if err = common.UpdateAnnotation(object, common.RestartedAt, currentTime); err != nil {
			return err
		}
		if err = r.Update(ctx, object); err != nil {
			return err
		}
	}
	return nil
}

// getNameservers finds the clusterDNS information in Cluster
func (r *DNSClassReconciler) getNameservers(ctx context.Context) ([]string, error) {
	// Try to get the information from ConfigMap
	var cm corev1.ConfigMap
	cmNamespacedName := types.NamespacedName{
		Name:      "kubelet-config",
		Namespace: "kube-system",
	}
	err := r.Get(ctx, cmNamespacedName, &cm)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	if !reflect.DeepEqual(cm, &corev1.ConfigMap{}) {
		kubeletData, ok := cm.DeepCopy().Data["kubelet"]
		if !ok {
			return nil, errors.New("kubelet-config does not contain `kubelet` key in data")
		}

		var kubeletConfig map[string]interface{}
		err := yaml.Unmarshal([]byte(kubeletData), &kubeletConfig)
		if err != nil {
			return nil, err
		}

		clusterDNSInterfaceSlice, ok := kubeletConfig["clusterDNS"].([]interface{})
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

	// TODO: Add more resource for detecting kube-dns IP if needed.

	return nil, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.DNSClass{}).
		WithEventFilter(&common.DnsClassPredicate{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconcilesForDNSClassReconciler}).
		Complete(r)
}
