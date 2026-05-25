// Package k8s is the client-go-backed adapter for the publisher's
// Kubernetes port at internal/client.
package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/meigma/yacd/containers/cardano-testnet/publisher/internal/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Config controls adapter construction.
type Config struct {
	// APIURL is the base URL of the Kubernetes API server. Any
	// trailing slash is trimmed by [New].
	APIURL string
	// TokenPath is the filesystem path to the bearer token file used
	// for authentication. client-go re-reads this on each request, so
	// rotated ServiceAccount tokens are honored automatically.
	TokenPath string
	// CAPath is the filesystem path to the PEM-encoded CA bundle used
	// for TLS verification of the API server.
	CAPath string
}

// Client implements [client.Client] against a Kubernetes API server
// using the client-go typed clientset.
type Client struct {
	clientset *kubernetes.Clientset
}

// Compile-time interface check.
var _ client.Client = (*Client)(nil)

// New constructs a Client from cfg. New does not contact the API
// server; transport setup is lazy inside client-go.
func New(cfg Config) (*Client, error) {
	host := strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	if host == "" {
		return nil, fmt.Errorf("k8s: APIURL is required")
	}
	if strings.TrimSpace(cfg.TokenPath) == "" {
		return nil, fmt.Errorf("k8s: TokenPath is required")
	}

	restConfig := &rest.Config{
		Host:            host,
		BearerTokenFile: cfg.TokenPath,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile: cfg.CAPath,
		},
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("k8s: build clientset: %w", err)
	}
	return &Client{clientset: clientset}, nil
}

// PatchConfigMap applies patch as a JSON merge patch.
func (c *Client) PatchConfigMap(ctx context.Context, namespace, name string, patch client.ConfigMapPatch) error {
	body, err := marshalMergePatch(patch)
	if err != nil {
		return fmt.Errorf("k8s: marshal patch for configmap %s/%s: %w", namespace, name, err)
	}
	if _, err := c.clientset.CoreV1().ConfigMaps(namespace).Patch(ctx, name, types.MergePatchType, body, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("k8s: patch configmap %s/%s: %w", namespace, name, err)
	}
	return nil
}

// configMapPatchBody is the JSON shape of the merge patch sent to the
// Kubernetes API server.
type configMapPatchBody struct {
	Metadata configMapPatchMetadata `json:"metadata,omitempty"`
	Data     map[string]*string     `json:"data,omitempty"`
}

// configMapPatchMetadata is the metadata sub-object of the merge
// patch body.
type configMapPatchMetadata struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

// marshalMergePatch returns the compact JSON merge-patch bytes the
// API server expects for p.
func marshalMergePatch(p client.ConfigMapPatch) ([]byte, error) {
	return json.Marshal(buildBody(p))
}

// MarshalMergePatchIndented returns the indented JSON merge-patch
// bytes that [Client.PatchConfigMap] would send for patch. Intended
// for dry-run rendering and human inspection.
func MarshalMergePatchIndented(patch client.ConfigMapPatch) ([]byte, error) {
	return json.MarshalIndent(buildBody(patch), "", "  ")
}

// buildBody assembles the merge-patch body shape from a port-level
// patch. PruneData entries become nil pointers (serialized as null);
// SetData entries become pointers to copied string values.
func buildBody(p client.ConfigMapPatch) configMapPatchBody {
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
