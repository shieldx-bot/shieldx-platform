/*
Copyright 2025.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TenantSpec defines the desired state of Tenant
type TenantSpec struct {
	// Owners is a list of owner identities (email/OIDC subject/group).
	// +kubebuilder:validation:MinItems=1
	Owners []string `json:"owners"`

	Tier      string `json:"tier"`
	Isolation string `json:"isolation"`
}

// TenantStatus defines the observed state of Tenant.
type TenantStatus struct {
	// Phase is a simple, high-level summary of the tenant state.
	// +kubebuilder:validation:Enum=Pending;Ready;Error
	// +optional
	Phase string `json:"phase,omitempty"`

	// Namespace is the actual namespace created/managed for this tenant.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Conditions represent the current state of the Tenant resource.
	// +kubebuilder:validation:Optional
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Tenant is the Schema for the tenants API
type Tenant struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Tenant
	// +required
	Spec TenantSpec `json:"spec"`

	// status defines the observed state of Tenant
	// +optional
	Status TenantStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tenant{}, &TenantList{})
}
