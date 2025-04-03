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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
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
	*rest.Config
	record.EventRecorder

	MaxConcurrentReconcilesForDNSClassReconciler int
}

//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.kubedns-shepherd.io,resources=dnsclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=core,resources=nodes;nodes/proxy,verbs=get;list;watch

// Reconcile reconciles a DNSClass object
func (r *DNSClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling")

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

	r.Event(dnsclass, corev1.EventTypeNormal, "Reconciling", "Reconciliation started")

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

		// Call discoverFields to populate discoveredFields and identify any undiscovered fields
		var undiscoveredFields map[string]bool
		undiscoveredFields, statusMessage = r.discoverFields(ctx, discoveredFields)
		if undiscoveredFields != nil {
			for _, key := range dnsclass.ExtractTemplateKeysRegex() {
				if undiscoveredFields[key] {
					err = errors.New(statusMessage)
					r.Event(dnsclass, corev1.EventTypeWarning, "Failed", "DNSClass will be unavailable due to missing required template keys")
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

	logger.Info("Reconciling successfully completed")
	r.Event(dnsclass, corev1.EventTypeNormal, "Successful", "Reconciliation successfully completed")

	return ctrl.Result{}, nil
}

// extractField retrieves a field from the kubelet configuration and asserts its type.
func (r *DNSClassReconciler) extractField(ctx context.Context, fieldName string, targetType string) (interface{}, error) {
	data, err := r.fetchNodeProxyConfigz(ctx)
	if err != nil {
		return nil, err
	}

	fieldValue, exists := data[fieldName]
	if !exists {
		return nil, fmt.Errorf("%s field is not found", fieldName)
	}

	switch targetType {
	case "string":
		value, ok := fieldValue.(string)
		if !ok {
			return nil, fmt.Errorf("%s field is not a string", fieldName)
		}
		return value, nil
	case "[]string":
		interfaceSlice, ok := fieldValue.([]interface{})
		if !ok {
			return nil, fmt.Errorf("%s field is not a slice", fieldName)
		}
		stringSlice := make([]string, len(interfaceSlice))
		for i, v := range interfaceSlice {
			str, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("element at index %d in %s is not a string", i, fieldName)
			}
			stringSlice[i] = str
		}
		return stringSlice, nil
	default:
		return nil, fmt.Errorf("unsupported target type: %s", targetType)
	}
}

// discoverFields retrieves and assigns cluster domain and nameservers to discoveredFields.
func (r *DNSClassReconciler) discoverFields(ctx context.Context, discoveredFields *configv1alpha1.DiscoveredFields) (map[string]bool, string) {
	var (
		fields       = make(map[string]bool, 2)
		messageSlice []string
	)

	if clusterDomain, err := r.extractField(ctx, "clusterDomain", "string"); err != nil {
		fields["clusterDomain"] = true
		messageSlice = append(messageSlice, err.Error())
	} else {
		discoveredFields.ClusterDomain = clusterDomain.(string)
	}

	if nameservers, err := r.extractField(ctx, "clusterDNS", "[]string"); err != nil {
		fields["nameservers"] = true
		messageSlice = append(messageSlice, err.Error())
	} else {
		discoveredFields.Nameservers = nameservers.([]string)
	}

	// Combine error messages into one if there were any
	if len(messageSlice) > 0 {
		return fields, fmt.Sprintf("discovery failed for the following fields: %s", strings.Join(messageSlice, "; "))
	}

	return nil, ""
}

// fetchNodeProxyConfig retrieves the kubelet proxy configuration from a random node
// Follow up for future improvement: https://github.com/stackabletech/issues/issues/662
func (r *DNSClassReconciler) fetchNodeProxyConfigz(ctx context.Context) (map[string]interface{}, error) {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodeList.Items) == 0 {
		return nil, errors.New("no nodes available to fetch config")
	}

	// Select the first node
	selectedNode := nodeList.Items[0].Name

	copyConfig := rest.CopyConfig(r.Config)
	if copyConfig.GroupVersion == nil {
		copyConfig.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
	}

	if copyConfig.NegotiatedSerializer == nil {
		copyConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	}

	// Create a REST client to make raw API requests
	restClient, err := rest.RESTClientFor(copyConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	// Construct the URL for the node's /configz endpoint
	url := fmt.Sprintf("/api/v1/nodes/%s/proxy/configz", selectedNode)
	result := restClient.Get().AbsPath(url).Do(ctx)
	rawData, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw data from node %s: %w", selectedNode, err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(rawData, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	kubeletConfig, ok := config["kubeletconfig"].(map[string]interface{})
	if !ok {
		return nil, errors.New("kubeletconfig field is not a map or is not found")
	}

	return kubeletConfig, nil
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
