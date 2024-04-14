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

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
)

// DNSClassReconciler reconciles a DNSClass object
type DNSClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	DisablePodReconciling                        bool
	MaxConcurrentReconcilesForDNSClassReconciler int
}

//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *DNSClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info(fmt.Sprintf("Reconciling for %s DNSClass", req.Name))

	dnsClass := &configv1alpha1.DNSClass{}
	err := r.Get(ctx, req.NamespacedName, dnsClass)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			logger.Info("DNSClass not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get the object")
		return ctrl.Result{}, err
	}

	// Mark the resource is not reconciled with `kubedns-shepherd.io/is-reconciled=false` annotation
	annotations := dnsClass.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	val, ok := annotations[IsReconciled]
	if !ok || val == "true" {
		annotations[IsReconciled] = "false"
		dnsClass.SetAnnotations(annotations)
		if err := r.Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to add annotations")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: reconcilePeriod}, nil
	}

	// Add default nameservers if not defined in object
	if dnsClass.Spec.DNSConfig.Nameservers == nil {
		dnsClass.Spec.DNSConfig.Nameservers, err = r.getNameservers(ctx)
		if err != nil {
			logger.Error(err, "Failed to set nameservers")
			return ctrl.Result{}, err
		}
		if err := r.Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to add nameservers")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: reconcilePeriod}, nil
	}

	// Set the status as Unknown when no status are available
	if dnsClass.Status.Conditions == nil || len(dnsClass.Status.Conditions) == 0 {
		meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: typeAvailable, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to update the status")
			return ctrl.Result{}, err
		}
	}

	if !controllerutil.ContainsFinalizer(dnsClass, finalizerString) {
		logger.Info("Adding Finalizer")
		if ok := controllerutil.AddFinalizer(dnsClass, finalizerString); !ok {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, dnsClass); err != nil {
			logger.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true, RequeueAfter: reconcilePeriod}, nil
	}

	if !dnsClass.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(dnsClass, finalizerString) {
			logger.Info("Performing Finalizer Operations before deleting DNSClass")

			// Add "Downgrade" status
			if !meta.IsStatusConditionPresentAndEqual(dnsClass.Status.Conditions, typeDegraded, metav1.ConditionUnknown) {
				meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: typeDegraded,
					Status: metav1.ConditionUnknown, Reason: "Finalizing",
					Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s", dnsClass.Name)})

				if err := r.Status().Update(ctx, dnsClass); err != nil {
					logger.Error(err, "Failed to update status")
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true, RequeueAfter: reconcilePeriod}, nil
			}

			// Revert the DNS Policy
			err := r.setDNSConfig(ctx, dnsClass, true)
			if err != nil {
				logger.Error(err, "Failed to revert the DNS Configuration")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: typeDegraded,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: "Finalizer operations were successfully accomplished"})

			if err := r.Status().Update(ctx, dnsClass); err != nil {
				logger.Error(err, "Failed to update the status")
				return ctrl.Result{}, err
			}

			logger.Info("Removing Finalizer after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(dnsClass, finalizerString); !ok {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, dnsClass); err != nil {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	err = r.setDNSConfig(ctx, dnsClass, false)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Mark the resource is already reconciled with `kubedns-shepherd.io/is-reconciled=true` annotation
	annotations[IsReconciled] = "true"
	dnsClass.SetAnnotations(annotations)

	if err := r.Update(ctx, dnsClass); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&dnsClass.Status.Conditions, metav1.Condition{Type: typeAvailable,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: "DNSClass were created and Workloads DNS configurations were updated"})

	if err := r.Status().Update(ctx, dnsClass); err != nil {
		logger.Error(err, "Failed to update the status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setDNSConfig
func (r *DNSClassReconciler) setDNSConfig(ctx context.Context, dnsClass *configv1alpha1.DNSClass, revert bool) error {
	for _, namespace := range dnsClass.Spec.Namespaces {
		var (
			pods corev1.PodList
			opts []client.ListOption
		)

		if namespace == "all" {
			opts = []client.ListOption{}

		} else {
			opts = []client.ListOption{
				client.InNamespace(namespace),
			}
		}

		err := r.List(ctx, &pods, opts...)
		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			podWorkload, err := getWorkloadObject(ctx, r.Client, &pod)
			if err != nil {
				if err.Error() == "unknown owner kind" {
					continue
				}
				return err
			}

			if r.DisablePodReconciling {
				if podWorkload.Kind == "Pod" {
					continue
				}
			}

			object, err := getObjectFromKindString(podWorkload.Kind)
			if err != nil {
				return err
			}

			if err = r.Get(ctx, types.NamespacedName{Name: podWorkload.Name, Namespace: podWorkload.Namespace}, object); err != nil {
				return err
			}

			err = r.configureDNSForWorkload(ctx, object, *dnsClass, revert)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *DNSClassReconciler) configureDNSForWorkload(ctx context.Context, object client.Object, dnsClass configv1alpha1.DNSClass, revert bool) error {
	var err error

	if revert {
		if err = setDNSConfig(object, &corev1.PodDNSConfig{}); err != nil {
			return err
		}

		if err = setDNSPolicyTo(object, corev1.DNSPolicy(dnsClass.Spec.ResetDNSPolicyTo)); err != nil {
			return err
		}

		if err = removeAnnotation(object, DNSConfigured); err != nil {
			return err
		}

		if err = removeAnnotation(object, DNSClassName); err != nil {
			return err
		}
	} else {
		if err = setDNSConfig(object, dnsClass.Spec.DNSConfig); err != nil {
			return err
		}

		if err = setDNSPolicyTo(object, corev1.DNSNone); err != nil {
			return err
		}

		if err = updateAnnotation(object, DNSConfigured, "true"); err != nil {
			return err
		}

		if err = updateAnnotation(object, DNSClassName, dnsClass.Name); err != nil {
			return err
		}
	}

	// Due to the restriction of pod update, we need to delete and recreate the object
	if object.GetObjectKind().GroupVersionKind().Kind == "Pod" {
		if err = r.Delete(ctx, object); err != nil {
			return err
		}
		// Wait until pod is deleted
		waitForPodDeletion(ctx, r.Client, types.NamespacedName{
			Name:      object.GetName(),
			Namespace: object.GetNamespace(),
		})
		// Remove `resourceVersion`
		object.SetResourceVersion("")
		if err = r.Create(ctx, object); err != nil {
			return err
		}
	} else {
		if err = r.Update(ctx, object); err != nil {
			// We ignore conflicts for multiple pod workloads.
			if !apierrors.IsConflict(err) {
				return err
			}
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
			return nil, errors.New("kubelet-config does not contain kubelet data")
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

	// TODO: Add more resource for detecting kube-dns endpoint.

	return nil, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.DNSClass{}).
		WithEventFilter(&dnsClassPredicate{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconcilesForDNSClassReconciler}).
		Complete(r)
}
