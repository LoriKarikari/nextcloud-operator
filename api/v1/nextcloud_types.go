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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NextcloudSpec defines the desired state of Nextcloud
type NextcloudSpec struct {
	// Version specifies the Nextcloud version to deploy
	// +kubebuilder:validation:Pattern="^[0-9]+\\.[0-9]+\\.[0-9]+$"
	// +kubebuilder:default="29.0.8"
	Version string `json:"version"`

	// Replicas specifies the number of Nextcloud pods
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
}

// NextcloudStatus defines the observed state of Nextcloud.
type NextcloudStatus struct {
	// Phase represents the current deployment phase
	// +kubebuilder:validation:Enum=Pending;Installing;Ready;Upgrading;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// conditions represent the current state of the Nextcloud resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Nextcloud is the Schema for the nextclouds API
type Nextcloud struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Nextcloud
	// +required
	Spec NextcloudSpec `json:"spec"`

	// status defines the observed state of Nextcloud
	// +optional
	Status NextcloudStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NextcloudList contains a list of Nextcloud
type NextcloudList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Nextcloud `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Nextcloud{}, &NextcloudList{})
}
