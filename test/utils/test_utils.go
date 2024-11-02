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

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	ClusterDomain = "cluster.local"
	ClusterDNS    = "10.96.0.10"

	NodeName = "dummy-node"

	dnsclassGenerateName = "test-dnsclass-"
)

var MockResponse interface{} = map[string]interface{}{
	"kubeletconfig": map[string]interface{}{
		"clusterDomain": ClusterDomain,
		"clusterDNS":    []string{ClusterDNS},
	},
}

// Starts a mock server to simulate the kubelet /configz endpoint
var MockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == fmt.Sprintf("/api/v1/nodes/%s/proxy/configz", NodeName) {
		w.Header().Set("Content-Type", "application/json")
		if MockResponse == nil {
			MockResponse = map[string]interface{}{
				"kubeletconfig": map[string]interface{}{
					"clusterDomain": ClusterDomain,
					"clusterDNS":    []string{ClusterDNS},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(MockResponse)
	} else {
		http.NotFound(w, r)
	}
}))

func CreateDNSClass(ctx context.Context, k8sClient client.Client, dnsclass client.Object) error {
	// Check if a specific name is provided
	if dnsclass.GetName() != "" {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: dnsclass.GetName()}, dnsclass)
		if err != nil {
			// Return error if it's not a NotFound error
			if !apierrors.IsNotFound(err) {
				return err
			}
			// If not found, create the object
			if err = k8sClient.Create(ctx, dnsclass); err != nil {
				// Return error if it's not an AlreadyExists error
				if !apierrors.IsAlreadyExists(err) {
					return nil
				}
				return err
			}
		}
	} else {
		// If no specific name is provided, directly set the GenerateName and create the object
		dnsclass.SetGenerateName(dnsclassGenerateName)
		if err := k8sClient.Create(ctx, dnsclass); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func DeleteDNSClass(ctx context.Context, k8sClient client.Client, dnsclass client.Object) error {
	if dnsclass.GetName() != "" {
		// If the resource updated, it won't be removed
		// due to the error of the object has been modified
		// So, we re-fetch the resource before remove it.
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: dnsclass.GetName()}, dnsclass); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		if err := k8sClient.Delete(ctx, dnsclass); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func GetDNSClass(ctx context.Context, k8sClient client.Client, dnsclass client.Object) error {
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: dnsclass.GetName()}, dnsclass); err != nil {
		return err
	}
	return nil
}

func AddNode(ctx context.Context, k8sClient client.Client) error {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: NodeName,
		},
	}
	err := k8sClient.Create(ctx, node)
	if err != nil {
		return err
	}
	return nil
}

func DeleteNode(ctx context.Context, k8sClient client.Client) error {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: NodeName,
		},
	}
	err := k8sClient.Delete(ctx, node)
	if err != nil {
		return err
	}
	return nil
}

func GetDummyConfig(url string, k8sManager manager.Manager) *rest.Config {
	dummyConfig := rest.CopyConfig(k8sManager.GetConfig())
	dummyConfig.Host = url
	return dummyConfig
}
