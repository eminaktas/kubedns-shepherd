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

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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

	// Annotations to be used in resources
	DNSClassName = "kubedns-shepherd.io/dns-class-name"
	IsReconciled = "kubedns-shepherd.io/is-reconciled"
)

// Workload used to define objects
type Workload struct {
	Name      string
	Namespace string
	Kind      string
}

// GetConfigMapData gets specified data in given ConfigMap from kube-system namespace
func GetConfigMapData(ctx context.Context, c client.Client, configMapName, keyName string) (map[string]interface{}, error) {
	cmNamespacedName := types.NamespacedName{Name: configMapName, Namespace: "kube-system"}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, cmNamespacedName, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%s not found in cluster", configMapName)
		}
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

// UpdateAnnotation updates or adds an annotation to a Kubernetes object
func UpdateAnnotation(obj client.Object, key, value string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	obj.SetAnnotations(annotations)
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

// Delete always returns false for DNSClass deletion events
func (*DnsClassPredicate) Delete(e event.DeleteEvent) bool {
	return false
}

// Generic always returns false for generic events
func (*DnsClassPredicate) Generic(e event.GenericEvent) bool {
	return false
}
