//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DNSClass) DeepCopyInto(out *DNSClass) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DNSClass.
func (in *DNSClass) DeepCopy() *DNSClass {
	if in == nil {
		return nil
	}
	out := new(DNSClass)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DNSClass) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DNSClassCustomDefaulter) DeepCopyInto(out *DNSClassCustomDefaulter) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DNSClassCustomDefaulter.
func (in *DNSClassCustomDefaulter) DeepCopy() *DNSClassCustomDefaulter {
	if in == nil {
		return nil
	}
	out := new(DNSClassCustomDefaulter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DNSClassCustomValidator) DeepCopyInto(out *DNSClassCustomValidator) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DNSClassCustomValidator.
func (in *DNSClassCustomValidator) DeepCopy() *DNSClassCustomValidator {
	if in == nil {
		return nil
	}
	out := new(DNSClassCustomValidator)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DNSClassList) DeepCopyInto(out *DNSClassList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DNSClass, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DNSClassList.
func (in *DNSClassList) DeepCopy() *DNSClassList {
	if in == nil {
		return nil
	}
	out := new(DNSClassList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DNSClassList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DNSClassSpec) DeepCopyInto(out *DNSClassSpec) {
	*out = *in
	if in.AllowedNamespaces != nil {
		in, out := &in.AllowedNamespaces, &out.AllowedNamespaces
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.DisabledNamespaces != nil {
		in, out := &in.DisabledNamespaces, &out.DisabledNamespaces
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.AllowedDNSPolicies != nil {
		in, out := &in.AllowedDNSPolicies, &out.AllowedDNSPolicies
		*out = make([]v1.DNSPolicy, len(*in))
		copy(*out, *in)
	}
	if in.DNSConfig != nil {
		in, out := &in.DNSConfig, &out.DNSConfig
		*out = new(v1.PodDNSConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DNSClassSpec.
func (in *DNSClassSpec) DeepCopy() *DNSClassSpec {
	if in == nil {
		return nil
	}
	out := new(DNSClassSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DNSClassStatus) DeepCopyInto(out *DNSClassStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.DiscoveredFields != nil {
		in, out := &in.DiscoveredFields, &out.DiscoveredFields
		*out = new(DiscoveredFields)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DNSClassStatus.
func (in *DNSClassStatus) DeepCopy() *DNSClassStatus {
	if in == nil {
		return nil
	}
	out := new(DNSClassStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveredFields) DeepCopyInto(out *DiscoveredFields) {
	*out = *in
	if in.Nameservers != nil {
		in, out := &in.Nameservers, &out.Nameservers
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveredFields.
func (in *DiscoveredFields) DeepCopy() *DiscoveredFields {
	if in == nil {
		return nil
	}
	out := new(DiscoveredFields)
	in.DeepCopyInto(out)
	return out
}