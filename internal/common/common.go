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

package common

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
)

// Definitions
const (
	// typeAvailable represents the status of the object reconciliation
	TypeAvailable = "Available"
	// typeDegraded represents the status used when DNSClass is deleted and the finalizer operations are must to occur.
	TypeDegraded = "Degraded"

	ReconcilePeriod = 1 * time.Second

	FinalizerString = "config.kubedns-shepherd.io/finalizer"

	// Annotations to be used in resources
	DNSClassName = "kubedns-shepherd.io/dns-class-name"
	IsReconciled = "kubedns-shepherd.io/is-reconciled"
	RestartedAt  = "kubedns-shepherd.io/restartedAt"

	DeploymentStr  = "Deployment"
	DaemonsetStr   = "DaemonSet"
	StatefulsetStr = "StatefulSet"
	ReplicasetStr  = "ReplicaSet"
	PodStr         = "Pod"
)

var (
	errUnkownKind = errors.New("unknown kind")
)

// Workload used to define objects
type Workload struct {
	Name      string
	Namespace string
	Kind      string
}

// GetDNSClass finds the DNSClass for the pod object with its namespace
func GetDNSClass(ctx context.Context, c client.Client, podNamespace string) (configv1alpha1.DNSClass, error) {
	var (
		dnsClass     configv1alpha1.DNSClass
		dnsClassList configv1alpha1.DNSClassList
	)

	err := c.List(ctx, &dnsClassList)
	if err != nil {
		return configv1alpha1.DNSClass{}, err
	}

	for _, dnsClassItem := range dnsClassList.Items {
		if slices.Contains(dnsClassItem.Spec.DisabledNamespaces, podNamespace) {
			continue
		}
		if slices.Contains(dnsClassItem.Spec.AllowedNamespaces, podNamespace) {
			dnsClass = dnsClassItem
			break // Exit the loop once a DNSClass is found
		}
		if dnsClassItem.Spec.AllowedNamespaces == nil {
			dnsClass = dnsClassItem
		}
	}

	if reflect.DeepEqual(dnsClass, configv1alpha1.DNSClass{}) {
		return configv1alpha1.DNSClass{}, errors.New("no matching DNSClass found for namespace: " + podNamespace)
	}
	return dnsClass, nil
}

// ListWorkloadObjects creates a Workload list for matching resources
func ListWorkloadObjects(ctx context.Context, c client.Client, dnsClass *configv1alpha1.DNSClass) ([]*Workload, error) {
	logger := log.FromContext(ctx)

	var (
		pods      corev1.PodList
		opts      []client.ListOption
		workloads []*Workload
		seen      map[string]struct{} = make(map[string]struct{})
	)

	allowedNamespaces := dnsClass.Spec.AllowedNamespaces
	// If allowedNamespaces is empty, add an item to start the for loop
	if allowedNamespaces == nil {
		allowedNamespaces = []string{"all"}
	}

	for _, namespace := range allowedNamespaces {
		if namespace == "all" {
			opts = nil
		} else {
			opts = []client.ListOption{
				client.InNamespace(namespace),
			}
		}

		err := c.List(ctx, &pods, opts...)
		if err != nil {
			return nil, err
		}

		for _, pod := range pods.Items {
			if slices.Contains(dnsClass.Spec.DisabledNamespaces, pod.Namespace) {
				continue
			}
			workload, err := getOwnerObject(ctx, c, &pod)
			if err != nil {
				logger.Info(fmt.Sprintf("Failed to get owner object for %s/%s. Skipping update for this resource.", pod.Namespace, pod.Name), "error", err)
				continue
			}
			key := fmt.Sprintf("%s-%s-%s", workload.Kind, workload.Namespace, workload.Name)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				workloads = append(workloads, workload)
			}
		}
	}
	return workloads, nil
}

func getOwnerObject(ctx context.Context, c client.Client, pod *corev1.Pod) (*Workload, error) {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == ReplicasetStr {
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
				if rsOwner.Kind == DeploymentStr || rsOwner.Kind == DaemonsetStr || rsOwner.Kind == StatefulsetStr {
					return &Workload{
						Name:      rsOwner.Name,
						Namespace: pod.Namespace,
						Kind:      rsOwner.Kind,
					}, nil
				}
			}
		} else if owner.Kind == DaemonsetStr || owner.Kind == DeploymentStr || owner.Kind == StatefulsetStr {
			return &Workload{
				Name:      owner.Name,
				Namespace: pod.Namespace,
				Kind:      owner.Kind,
			}, nil
		} else {
			return nil, errors.New("unknown owner kind: " + owner.Kind)
		}
	}

	return &Workload{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Kind:      pod.Kind,
	}, nil
}

// GetObjectFromKindString converts a string to a Kubernetes object type
func GetObjectFromKindString(kind string) (client.Object, error) {
	switch kind {
	case DeploymentStr:
		return &appsv1.Deployment{}, nil
	case StatefulsetStr:
		return &appsv1.StatefulSet{}, nil
	case DaemonsetStr:
		return &appsv1.DaemonSet{}, nil
	case ReplicasetStr:
		return &appsv1.ReplicaSet{}, nil
	case PodStr:
		return &corev1.Pod{}, nil
	default:
		return nil, errUnkownKind
	}
}

// RemoveAnnotation removes an annotation from a Kubernetes object
func RemoveAnnotation(obj client.Object, key string) error {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		delete(o.Spec.Template.Annotations, key)
		return nil
	case *appsv1.StatefulSet:
		delete(o.Spec.Template.Annotations, key)
		return nil
	case *appsv1.DaemonSet:
		delete(o.Spec.Template.Annotations, key)
		return nil
	case *appsv1.ReplicaSet:
		delete(o.Spec.Template.Annotations, key)
		return nil
	case *corev1.Pod:
		delete(o.Annotations, key)
		return nil
	default:
		return errUnkownKind
	}
}

// UpdateAnnotation updates or adds an annotation to a Kubernetes object
func UpdateAnnotation(obj client.Object, key, value string) error {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		if o.Spec.Template.Annotations == nil {
			o.Spec.Template.Annotations = make(map[string]string)
		}
		o.Spec.Template.Annotations[key] = value
		return nil
	case *appsv1.StatefulSet:
		if o.Spec.Template.Annotations == nil {
			o.Spec.Template.Annotations = make(map[string]string)
		}
		o.Spec.Template.Annotations[key] = value
		return nil
	case *appsv1.DaemonSet:
		if o.Spec.Template.Annotations == nil {
			o.Spec.Template.Annotations = make(map[string]string)
		}
		o.Spec.Template.Annotations[key] = value
		return nil
	case *appsv1.ReplicaSet:
		if o.Spec.Template.Annotations == nil {
			o.Spec.Template.Annotations = make(map[string]string)
		}
		o.Spec.Template.Annotations[key] = value
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

// DnsClassPredicate is a predicate for DNSClass objects
type DnsClassPredicate struct {
	predicate.Funcs
}

// Create checks if a DNSClass object is marked as reconciled
func (*DnsClassPredicate) Create(e event.CreateEvent) bool {
	annotations := e.Object.GetAnnotations()
	if val, ok := annotations[IsReconciled]; ok {
		return val == "false"
	}
	return true
}

// Update checks if a DNSClass object is updated
func (*DnsClassPredicate) Update(e event.UpdateEvent) bool {
	// If resources updated, we need to validate for DNS Configuration
	return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
}

// Delete always returns true for DNSClass deletion events
func (*DnsClassPredicate) Delete(e event.DeleteEvent) bool {
	if dnsClass, ok := e.Object.(*configv1alpha1.DNSClass); ok {
		if !dnsClass.Spec.EnablePodRestart {
			return false
		}
	}
	return true
}

// Generic always returns false for generic events
func (*DnsClassPredicate) Generic(e event.GenericEvent) bool {
	return false
}
