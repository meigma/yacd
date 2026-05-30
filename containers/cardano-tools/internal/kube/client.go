package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// defaultRequestTimeout is the per-request deadline applied to Kubernetes API
// calls when [Config.Timeout] is zero.
const defaultRequestTimeout = 30 * time.Second

// Client is the Kubernetes seam the report verb consumes.
type Client interface {
	// PatchConfigMap applies patch as a JSON merge patch to the ConfigMap at
	// namespace/name.
	PatchConfigMap(ctx context.Context, namespace, name string, patch ConfigMapPatch) error
}

// ConfigMapPatch describes the changes to apply to a ConfigMap.
type ConfigMapPatch struct {
	// SetData maps data keys to the UTF-8 values to set on the ConfigMap.
	SetData map[string]string
	// PruneData lists data keys whose values should be removed (set to null
	// in the merge patch).
	PruneData []string
	// Annotations is the map of metadata annotations to set on the ConfigMap.
	Annotations map[string]string
}

// Config controls [NewClient] construction.
type Config struct {
	// APIURL is the base URL of the Kubernetes API server. Any trailing slash
	// is trimmed by [NewClient].
	APIURL string
	// TokenPath is the filesystem path to the bearer token file. client-go
	// re-reads it on each request, so rotated ServiceAccount tokens are
	// honored automatically.
	TokenPath string
	// CAPath is the filesystem path to the PEM-encoded CA bundle used for TLS
	// verification of the API server.
	CAPath string
	// Timeout bounds each Kubernetes API call. Zero selects a 30-second
	// default.
	Timeout time.Duration
}

// Adapter implements [Client] against a Kubernetes API server using the
// client-go typed clientset.
type Adapter struct {
	clientset *kubernetes.Clientset
}

// Compile-time interface check.
var _ Client = (*Adapter)(nil)

// NewClient constructs an Adapter from cfg. It does not contact the API
// server; client-go sets up transport lazily.
func NewClient(cfg Config) (*Adapter, error) {
	host := strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	if host == "" {
		return nil, fmt.Errorf("kube: APIURL is required")
	}
	if strings.TrimSpace(cfg.TokenPath) == "" {
		return nil, fmt.Errorf("kube: TokenPath is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultRequestTimeout
	}
	restConfig := &rest.Config{
		Host:            host,
		BearerTokenFile: cfg.TokenPath,
		Timeout:         timeout,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile: cfg.CAPath,
		},
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("kube: build clientset: %w", err)
	}
	return &Adapter{clientset: clientset}, nil
}

// PatchConfigMap applies patch as a JSON merge patch.
func (a *Adapter) PatchConfigMap(ctx context.Context, namespace, name string, patch ConfigMapPatch) error {
	body, err := marshalMergePatch(patch)
	if err != nil {
		return fmt.Errorf("kube: marshal patch for configmap %s/%s: %w", namespace, name, err)
	}
	if _, err := a.clientset.CoreV1().ConfigMaps(namespace).Patch(ctx, name, types.MergePatchType, body, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("kube: patch configmap %s/%s: %w", namespace, name, err)
	}
	return nil
}

// configMapPatchBody is the JSON shape of the merge patch sent to the API
// server.
type configMapPatchBody struct {
	Metadata configMapPatchMetadata `json:"metadata,omitempty"`
	Data     map[string]*string     `json:"data,omitempty"`
}

// configMapPatchMetadata is the metadata sub-object of the merge-patch body.
type configMapPatchMetadata struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

// marshalMergePatch returns the compact JSON merge-patch bytes the API server
// expects for p.
func marshalMergePatch(p ConfigMapPatch) ([]byte, error) {
	return json.Marshal(buildBody(p))
}

// MarshalMergePatchIndented returns the indented JSON merge-patch bytes that
// [Adapter.PatchConfigMap] would send for patch. Intended for dry-run
// rendering and human inspection.
func MarshalMergePatchIndented(patch ConfigMapPatch) ([]byte, error) {
	return json.MarshalIndent(buildBody(patch), "", "  ")
}

// buildBody assembles the merge-patch body from a port-level patch. PruneData
// entries become nil pointers (serialized as null); SetData entries become
// pointers to copied string values.
func buildBody(p ConfigMapPatch) configMapPatchBody {
	var data map[string]*string
	if len(p.SetData) > 0 || len(p.PruneData) > 0 {
		data = make(map[string]*string, len(p.SetData)+len(p.PruneData))
		for key, value := range p.SetData {
			copied := value
			data[key] = &copied
		}
		for _, key := range p.PruneData {
			data[key] = nil
		}
	}
	return configMapPatchBody{
		Metadata: configMapPatchMetadata{Annotations: p.Annotations},
		Data:     data,
	}
}
