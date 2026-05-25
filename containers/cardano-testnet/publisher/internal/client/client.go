// Package client defines the Kubernetes interface the publisher
// consumes. Adapters fulfilling this contract live in subpackages such
// as internal/client/k8s.
package client

import "context"

// Client is the Kubernetes interface the publisher consumes.
type Client interface {
	// PatchConfigMap applies patch as a JSON merge patch to the
	// ConfigMap at namespace/name.
	PatchConfigMap(ctx context.Context, namespace, name string, patch ConfigMapPatch) error
}

// ConfigMapPatch describes the changes to apply to a ConfigMap.
type ConfigMapPatch struct {
	// SetData maps data keys to the UTF-8 values that should be set on
	// the ConfigMap.
	SetData map[string]string
	// PruneData lists data keys whose values should be removed (set to
	// null in the merge patch).
	PruneData []string
	// Annotations is the map of metadata annotations that should be
	// set on the ConfigMap.
	Annotations map[string]string
}
