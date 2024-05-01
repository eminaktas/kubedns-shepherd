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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DNSClassSpec defines the desired state of DNSClass
type DNSClassSpec struct {
	AllowedNamespaces  []string             `json:"allowedNamespaces,omitempty"`
	DisabledNamespaces []string             `json:"disabledNamespaces,omitempty"`
	AllowedDNSPolicies []corev1.DNSPolicy   `json:"allowedDNSPolicies,omitempty"`
	DNSPolicy          corev1.DNSPolicy     `json:"dnsPolicy,omitempty"`
	DNSConfig          *corev1.PodDNSConfig `json:"dnsConfig,omitempty"`
	DiscoveredFields   *DiscoveredFields    `json:"discoveredFields,omitempty"`
}

type DiscoveredFields struct {
	Nameservers   []string `json:"nameservers,omitempty"`
	ClusterDomain string   `json:"clusterDomain,omitempty"`
	ClusterName   string   `json:"clusterName,omitempty"`
	DNSDomain     string   `json:"dnsDomain,omitempty"`
}

// DNSClassStatus defines the observed state of DNSClass
type DNSClassStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName="dc"
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[*].status"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DNSClass is the Schema for the dnsclasses API
type DNSClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSClassSpec   `json:"spec,omitempty"`
	Status DNSClassStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DNSClassList contains a list of DNSClass
type DNSClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DNSClass{}, &DNSClassList{})
}
