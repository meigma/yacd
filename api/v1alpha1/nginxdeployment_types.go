package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NginxDeploymentSpec defines the desired state of NginxDeployment.
type NginxDeploymentSpec struct {
	// Image is the nginx container image to run.
	// +kubebuilder:default="nginxinc/nginx-unprivileged:stable"
	// +optional
	Image string `json:"image,omitempty"`

	// Replicas is the desired number of nginx pods.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Port is the container port nginx listens on.
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// Config is the nginx configuration file content.
	// +kubebuilder:validation:MaxLength=65536
	// +optional
	Config string `json:"config,omitempty"`
}

// NginxDeploymentStatus defines the observed state of NginxDeployment.
type NginxDeploymentStatus struct {
	// ReadyReplicas is the number of nginx pods currently ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// conditions represent the current state of the NginxDeployment resource.
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
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63",message="metadata.name must be 63 characters or less because owned resources are named after the NginxDeployment"
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$')",message="metadata.name must be an RFC 1123 label because owned Services are named after the NginxDeployment"

// NginxDeployment is the Schema for the nginxdeployments API.
type NginxDeployment struct {
	// TypeMeta carries the API group, version, and kind for this resource.
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NginxDeployment
	// +required
	Spec NginxDeploymentSpec `json:"spec"`

	// status defines the observed state of NginxDeployment
	// +optional
	Status NginxDeploymentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NginxDeploymentList contains a list of NginxDeployment.
type NginxDeploymentList struct {
	// TypeMeta carries the API group, version, and kind for this list.
	metav1.TypeMeta `json:",inline"`

	// ListMeta is the standard list metadata, including resourceVersion and continue token.
	metav1.ListMeta `json:"metadata,omitzero"`

	// Items holds the NginxDeployment resources returned in this list.
	Items []NginxDeployment `json:"items"`
}
