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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Definitions
const (
	// typeAvailable represents the status of the object reconciliation
	TypeAvailable = "Available"
	// typeDegraded represents the status used when DNSClass is deleted and the finalizer operations are must to occur.
	TypeDegraded = "Degraded"

	ReconcilePeriod = 1 * time.Second

	FinalizerString = "config.kubedns-shepherd.io/finalizer"

	// Annotation keys
	DNSConfigurationDisabled = "kubedns-shepherd.io/dns-configuration-disabled"
	DNSConfigured            = "kubedns-shepherd.io/dns-configured"
	DNSClassName             = "kubedns-shepherd.io/dns-class-name"
	IsReconciled             = "kubedns-shepherd.io/is-reconciled"

	deploymentStr  = "Deployment"
	daemonsetStr   = "DaemonSet"
	statefulsetStr = "StatefulSet"
	replicasetStr  = "ReplicaSet"
	podStr         = "Pod"

	trueStr  = "false"
	falseStr = "false"
)

var (
	errUnkownKind      = errors.New("unknown kind")
	errUnkownOwnerKind = errors.New("unknown owner kind")
)

// Define a function to wait for a Pod to be deleted
func waitForPodDeletion(ctx context.Context, c client.Client, podName types.NamespacedName) error {
	for {
		// Check if the Pod still exists
		pod := &corev1.Pod{}
		err := c.Get(ctx, podName, pod)
		if err != nil && apierrors.IsNotFound(err) {
			// Pod has been deleted
			return nil
		} else if err != nil {
			// Error occurred while checking for Pod
			return err
		}

		// Pod still exists, wait for a short duration before checking again
		time.Sleep(ReconcilePeriod)
	}
}

func getWorkloadObject(ctx context.Context, c client.Client, pod *corev1.Pod) (*Workload, error) {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == replicasetStr {
			var rs appsv1.ReplicaSet
			err := c.Get(ctx, client.ObjectKey{
				Namespace: pod.Namespace,
				Name:      owner.Name,
			}, &rs)
			if err != nil {
				return nil, err
			}

			if rs.OwnerReferences == nil {
				return &Workload{
					Name:      owner.Name,
					Namespace: pod.Namespace,
					Kind:      owner.Kind,
				}, nil
			}

			for _, rsOwner := range rs.OwnerReferences {
				if rsOwner.Kind == deploymentStr || rsOwner.Kind == daemonsetStr || rsOwner.Kind == statefulsetStr {
					return &Workload{
						Name:      rsOwner.Name,
						Namespace: pod.Namespace,
						Kind:      rsOwner.Kind,
					}, nil
				}
			}
		} else if owner.Kind == daemonsetStr || owner.Kind == deploymentStr || owner.Kind == statefulsetStr {
			return &Workload{
				Name:      owner.Name,
				Namespace: pod.Namespace,
				Kind:      owner.Kind,
			}, nil
		} else {
			return nil, errUnkownOwnerKind
		}
	}

	return &Workload{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Kind:      pod.Kind,
	}, nil
}

func setDNSPolicyTo(obj client.Object, dnsPolicy corev1.DNSPolicy) error {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		o.Spec.Template.Spec.DNSPolicy = dnsPolicy
		return nil
	case *appsv1.StatefulSet:
		o.Spec.Template.Spec.DNSPolicy = dnsPolicy
		return nil
	case *appsv1.DaemonSet:
		o.Spec.Template.Spec.DNSPolicy = dnsPolicy
		return nil
	case *appsv1.ReplicaSet:
		o.Spec.Template.Spec.DNSPolicy = dnsPolicy
		return nil
	case *corev1.Pod:
		o.Spec.DNSPolicy = dnsPolicy
		return nil
	default:
		return errUnkownKind
	}
}

func setDNSConfig(obj client.Object, dnsConfig *corev1.PodDNSConfig) error {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		o.Spec.Template.Spec.DNSConfig = dnsConfig
		return nil
	case *appsv1.StatefulSet:
		o.Spec.Template.Spec.DNSConfig = dnsConfig
		return nil
	case *appsv1.DaemonSet:
		o.Spec.Template.Spec.DNSConfig = dnsConfig
		return nil
	case *appsv1.ReplicaSet:
		o.Spec.Template.Spec.DNSConfig = dnsConfig
		return nil
	case *corev1.Pod:
		o.Spec.DNSConfig = dnsConfig
		return nil
	default:
		return errUnkownKind
	}
}

func getObjectFromKindString(kind string) (client.Object, error) {
	switch kind {
	case deploymentStr:
		return &appsv1.Deployment{}, nil
	case statefulsetStr:
		return &appsv1.StatefulSet{}, nil
	case daemonsetStr:
		return &appsv1.DaemonSet{}, nil
	case replicasetStr:
		return &appsv1.ReplicaSet{}, nil
	case podStr:
		return &corev1.Pod{}, nil
	default:
		return nil, errUnkownKind
	}
}

func getAnnotations(obj client.Object) map[string]string {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Annotations
	case *appsv1.StatefulSet:
		return o.Annotations
	case *appsv1.DaemonSet:
		return o.Annotations
	case *appsv1.ReplicaSet:
		return o.Annotations
	case *corev1.Pod:
		return o.Annotations
	default:
		return nil
	}
}

func getDNSPolicy(obj client.Object) corev1.DNSPolicy {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Spec.Template.Spec.DNSPolicy
	case *appsv1.StatefulSet:
		return o.Spec.Template.Spec.DNSPolicy
	case *appsv1.DaemonSet:
		return o.Spec.Template.Spec.DNSPolicy
	case *appsv1.ReplicaSet:
		return o.Spec.Template.Spec.DNSPolicy
	case *corev1.Pod:
		return o.Spec.DNSPolicy
	default:
		return ""
	}
}

func getDNSConfig(obj client.Object) *corev1.PodDNSConfig {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Spec.Template.Spec.DNSConfig
	case *appsv1.StatefulSet:
		return o.Spec.Template.Spec.DNSConfig
	case *appsv1.DaemonSet:
		return o.Spec.Template.Spec.DNSConfig
	case *appsv1.ReplicaSet:
		return o.Spec.Template.Spec.DNSConfig
	case *corev1.Pod:
		return o.Spec.DNSConfig
	default:
		return nil
	}
}

func isDNSConfigurable(obj client.Object) bool {
	annotations := getAnnotations(obj)
	if val, ok := annotations[DNSConfigurationDisabled]; ok {
		if val == trueStr {
			return true
		}
	}
	return false
}

// isDNSConfigured
func isDNSConfigured(obj client.Object, dnsClass *corev1.PodDNSConfig) bool {
	annotations := getAnnotations(obj)
	dnsPolicy := getDNSPolicy(obj)
	dnsConfig := getDNSConfig(obj)

	if val, ok := annotations[DNSConfigured]; ok {
		if val == trueStr {
			// Check if the DNS configuration is altered
			if dnsPolicy != corev1.DNSNone {
				return false
			}
			if dnsConfig == nil {
				return false
			}
			if result := reflect.DeepEqual(dnsConfig, dnsClass); result {
				return true
			}
		}
	}
	return false
}

func removeAnnotation(obj client.Object, key string) error {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		delete(o.Annotations, key)
		return nil
	case *appsv1.StatefulSet:
		delete(o.Annotations, key)
		return nil
	case *appsv1.DaemonSet:
		delete(o.Annotations, key)
		return nil
	case *appsv1.ReplicaSet:
		delete(o.Annotations, key)
		return nil
	case *corev1.Pod:
		delete(o.Annotations, key)
		return nil
	default:
		return errUnkownKind
	}
}

func updateAnnotation(obj client.Object, key, value string) error {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		if o.Annotations == nil {
			o.Annotations = make(map[string]string)
		}
		o.Annotations[key] = value
		return nil
	case *appsv1.StatefulSet:
		if o.Annotations == nil {
			o.Annotations = make(map[string]string)
		}
		o.Annotations[key] = value
		return nil
	case *appsv1.DaemonSet:
		if o.Annotations == nil {
			o.Annotations = make(map[string]string)
		}
		o.Annotations[key] = value
		return nil
	case *appsv1.ReplicaSet:
		if o.Annotations == nil {
			o.Annotations = make(map[string]string)
		}
		o.Annotations[key] = value
		return nil
	case *corev1.Pod:
		if o.Annotations == nil {
			o.Annotations = make(map[string]string)
		}
		o.Annotations[key] = value
		return nil
	default:
		return errUnkownKind
	}
}

type podPredicate struct {
	predicate.Funcs
}

func (*podPredicate) Create(e event.CreateEvent) bool {
	// On restarts, it receieves create events for running pods
	// which is enabled to process the DNS configuration.
	return true
}

func (*podPredicate) Update(e event.UpdateEvent) bool {
	// If resources updated, we need to check if DNS Configuration is changed.
	return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
}

func (*podPredicate) Delete(e event.DeleteEvent) bool {
	return false
}

func (*podPredicate) Generic(e event.GenericEvent) bool {
	return false
}

type dnsClassPredicate struct {
	predicate.Funcs
}

func (*dnsClassPredicate) Create(e event.CreateEvent) bool {
	annotations := e.Object.GetAnnotations()
	if val, ok := annotations[IsReconciled]; ok {
		return val == falseStr
	}
	return true
}

func (*dnsClassPredicate) Update(e event.UpdateEvent) bool {
	// If resources updated, we need to validate for DNS Configuration
	return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
}

func (*dnsClassPredicate) Delete(e event.DeleteEvent) bool {
	return true
}

func (*dnsClassPredicate) Generic(e event.GenericEvent) bool {
	return false
}
