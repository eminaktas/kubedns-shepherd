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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterDomain = "cluster.local"
	ClusterDNS    = "10.96.0.10"
	ClusterName   = "test-cluster"

	dnsclassGenerateName = "test-dnsclass-"
)

func CreateKubeADMConfigMap(ctx context.Context, k8sClient client.Client) error {
	kubeadmcm := &corev1.ConfigMap{}
	kubeadmNamespacedName := types.NamespacedName{
		Name:      "kubeadm-config",
		Namespace: metav1.NamespaceSystem,
	}

	err := k8sClient.Get(ctx, kubeadmNamespacedName, kubeadmcm)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		kubeadmConfig := fmt.Sprintf(`apiVersion: kubeadm.k8s.io/v1beta3
clusterName: %s
kind: ClusterConfiguration
kubernetesVersion: v1.31.0
networking:
  dnsDomain: %s
`, ClusterName, ClusterDomain)

		kubeadmcm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubeadmNamespacedName.Name,
				Namespace: kubeadmNamespacedName.Namespace,
			},
			Data: map[string]string{
				"ClusterConfiguration": kubeadmConfig,
			},
		}

		if err := k8sClient.Create(ctx, kubeadmcm); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func DeleteKubeADMConfigMap(ctx context.Context, k8sClient client.Client) error {
	kubeadm := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: metav1.NamespaceSystem, Name: "kubeadm-config"}, kubeadm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := k8sClient.Delete(ctx, kubeadm); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func GetKubeADMConfigMap(ctx context.Context, k8sClient client.Client) error {
	kubeadm := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: metav1.NamespaceSystem, Name: "kubeadm-config"}, kubeadm); err != nil {
		return err
	}
	return nil
}

func CreateKubeletConfigMap(ctx context.Context, k8sClient client.Client) error {
	kubeletcm := &corev1.ConfigMap{}
	kubeletNamespacedName := types.NamespacedName{
		Name:      "kubelet-config",
		Namespace: metav1.NamespaceSystem,
	}

	err := k8sClient.Get(ctx, kubeletNamespacedName, kubeletcm)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		kubeletConfig := fmt.Sprintf(`apiVersion: kubelet.config.k8s.io/v1beta1
clusterDNS:
- %s
clusterDomain: %s
kind: KubeletConfiguration
`, ClusterDNS, ClusterDomain)

		kubeletcm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubeletNamespacedName.Name,
				Namespace: kubeletNamespacedName.Namespace,
			},
			Data: map[string]string{
				"kubelet": kubeletConfig,
			},
		}

		if err := k8sClient.Create(ctx, kubeletcm); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func DeleteKubeletConfigMap(ctx context.Context, k8sClient client.Client) error {
	kubeletcm := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: metav1.NamespaceSystem, Name: "kubelet-config"}, kubeletcm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := k8sClient.Delete(ctx, kubeletcm); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func GetKubeletConfigMap(ctx context.Context, k8sClient client.Client) error {
	kubelet := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: metav1.NamespaceSystem, Name: "kubelet-config"}, kubelet); err != nil {
		return err
	}
	return nil
}

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
