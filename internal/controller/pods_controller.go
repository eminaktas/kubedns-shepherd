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

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Workload struct {
	Name      string
	Namespace string
	Kind      string
}

// PodsReconciler reconciles a Pods object
type PodsReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	DisablePodReconciling                    bool
	MaxConcurrentReconcilesForPodsReconciler int
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *PodsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Each created or updated pod resource, reconciling will start and mechanism will work
	var pod corev1.Pod
	err := r.Get(ctx, req.NamespacedName, &pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("object not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get pod object")
	}

	podWorkload, err := getWorkloadObject(ctx, r.Client, &pod)
	if err != nil {
		logger.Info("Unsupported workload object", "error", err)
		return ctrl.Result{}, nil
	}

	if r.DisablePodReconciling {
		if podWorkload.Kind == "Pod" {
			return ctrl.Result{}, nil
		}
	}

	dnsConfigurationDisabled := isDNSConfigurable(&pod)
	if dnsConfigurationDisabled {
		logger.Info(fmt.Sprintf("DNS configuration has been disabled with %s=true annotation", DNSConfigurationDisabled))
		return ctrl.Result{}, nil
	}

	// Get DNSClass object
	var dnsClass configv1alpha1.DNSClass
	var dnsClassList configv1alpha1.DNSClassList
	err = r.List(ctx, &dnsClassList)
	if err != nil {
		logger.Error(err, "Failed to list DNSClass objects")
		return ctrl.Result{}, err
	}

	for _, val := range dnsClassList.Items {
		if slices.Contains(val.Spec.Namespaces, pod.Namespace) {
			dnsClass = val
			break
		}
		if slices.Contains(val.Spec.Namespaces, "all") {
			dnsClass = val
		}
	}

	// If no DNSClass found, stop reconciling
	if reflect.DeepEqual(dnsClass, configv1alpha1.DNSClass{}) {
		logger.Info("No DNSClass found for the object")
		return ctrl.Result{}, nil
	}

	// Get the latest condition
	count := len(dnsClass.Status.Conditions)
	if count == 0 {
		return ctrl.Result{Requeue: true, RequeueAfter: ReconcilePeriod}, nil
	}
	latestCondition := dnsClass.Status.Conditions[count-1]

	// Check if DNSClass has been marked as `Downgrade` which is used when DNSClass is being deleted.
	if latestCondition.Type == TypeDegraded {
		logger.Info(fmt.Sprintf("%s DNSClass has been marked to be deleted", dnsClass.Name))
		return ctrl.Result{}, nil
	}

	annotations := dnsClass.GetAnnotations()
	if annotations == nil {
		return ctrl.Result{}, nil
	}
	if isReconciledAnnotation, ok := annotations[IsReconciled]; ok {
		if isReconciledAnnotation == "false" {
			return ctrl.Result{}, nil
		}
	}

	// Check if DNSClass condition type is Available and status is True, othewise, stop reconciling
	if latestCondition.Type != TypeAvailable && latestCondition.Status != v1.ConditionTrue {
		logger.Info(fmt.Sprintf("%s DNSClass is being reconciled", dnsClass.Name))
		return ctrl.Result{}, nil
	}

	alreadyDNSConfigured := isDNSConfigured(&pod, dnsClass.Spec.DNSConfig)
	if alreadyDNSConfigured {
		logger.Info("DNS configuration has been configured, no need to reconcile")
		return ctrl.Result{}, nil
	}

	// Configure Pod Object
	err = r.configureDNSForWorkload(ctx, podWorkload, dnsClass)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to configure %+v object", *podWorkload))
		return ctrl.Result{Requeue: true, RequeueAfter: ReconcilePeriod}, nil
	}

	logger.Info(fmt.Sprintf("DNSConfig configured for %+v with %s DNSClass", *podWorkload, dnsClass.Name))

	return ctrl.Result{}, nil
}

func (r *PodsReconciler) configureDNSForWorkload(ctx context.Context, podWorkload *Workload, dnsClass configv1alpha1.DNSClass) error {
	object, err := getObjectFromKindString(podWorkload.Kind)
	if err != nil {
		return err
	}

	if err = r.Get(ctx, types.NamespacedName{Name: podWorkload.Name, Namespace: podWorkload.Namespace}, object); err != nil {
		return err
	}

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

	// Due to the restriction of pod update, we need to delete and recreate the object
	if podWorkload.Kind == "Pod" {
		if err = r.Delete(ctx, object); err != nil {
			return err
		}
		// Wait until pod is deleted
		if err := waitForPodDeletion(ctx, r.Client, types.NamespacedName{
			Name:      podWorkload.Name,
			Namespace: podWorkload.Namespace,
		}); err != nil {
			return err
		}
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

// SetupWithManager sets up the controller with the Manager.
func (r *PodsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(&podPredicate{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconcilesForPodsReconciler}).
		Complete(r)
}
