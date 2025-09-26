package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretReference represents a reference to a secret
type SecretReference struct {
	// Name of the secret
	Name string `json:"name"`

	// Key within the secret containing the password
	// +kubebuilder:default="password"
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`

	// Key within the secret containing the username
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`
}

// AdminSpec defines admin user configuration
type AdminSpec struct {
	// Username for the admin user
	// +kubebuilder:default="admin"
	// +optional
	Username string `json:"username,omitempty"`

	// SecretRef references a Secret containing admin credentials
	// If not provided, a secret will be auto-generated
	// Required keys: password (and optionally username)
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

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

	// Admin user configuration
	// +optional
	Admin *AdminSpec `json:"admin,omitempty"`
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
