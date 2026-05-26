package kube

import (
	"context"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client is the Kubernetes behaviour the CLI command layer depends on. It is
// the port; the concrete Adapter below is the controller-runtime-backed
// implementation. Tests substitute a mock.
type Client interface {
	// DefaultNamespace returns the namespace selected by the user's
	// kubeconfig, falling back to "default".
	DefaultNamespace() string

	// ApplyCardanoNetwork performs server-side apply of the provided
	// CardanoNetwork with the CLI as field owner.
	ApplyCardanoNetwork(ctx context.Context, network *yacdv1alpha1.CardanoNetwork) error

	// GetCardanoNetwork fetches the named CardanoNetwork. A NotFound result
	// is translated into a typed error message naming namespace and name.
	GetCardanoNetwork(ctx context.Context, namespace string, name string) (*yacdv1alpha1.CardanoNetwork, error)

	// GetSecretValue reads a single key from a Secret. Missing Secret or
	// missing key are surfaced as a typed error.
	GetSecretValue(ctx context.Context, namespace string, name string, key string) (string, error)
}

// Adapter is the controller-runtime-backed implementation of Client. It is
// returned as a concrete value from NewClient so the construction site holds
// a typed lifecycle handle; the command layer holds the Client interface.
type Adapter struct {
	client    client.Client
	namespace string
}

// NewClient constructs an Adapter from the user's kubeconfig. The returned
// concrete type satisfies the Client port. Callers that need a port-typed
// value (for example, dependency injection in tests) assign the result
// through a function wrapper at the construction site.
func NewClient(config Config) (*Adapter, error) {
	restCfg, namespace, err := restConfig(config)
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yacdv1alpha1.AddToScheme(scheme))

	kubeClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	return &Adapter{
		client:    kubeClient,
		namespace: namespace,
	}, nil
}

// DefaultNamespace returns the namespace the adapter resolved at construction
// time, with the "default" fallback if the kubeconfig selected nothing.
func (a *Adapter) DefaultNamespace() string {
	if strings.TrimSpace(a.namespace) == "" {
		return defaultNamespace
	}

	return a.namespace
}

// ApplyCardanoNetwork applies the CardanoNetwork through server-side apply.
// The Patch call is intentionally still client.Apply rather than the
// generated apply-config path because the CRD does not ship one yet.
func (a *Adapter) ApplyCardanoNetwork(ctx context.Context, network *yacdv1alpha1.CardanoNetwork) error {
	if network == nil {
		return fmt.Errorf("cardanonetwork is required")
	}
	//nolint:staticcheck // client.Apply is still the practical SSA path for CRD object structs without generated apply configurations.
	if err := a.client.Patch(ctx, network.DeepCopy(), client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply cardanonetwork %s/%s: %w", network.Namespace, network.Name, err)
	}

	return nil
}

// GetCardanoNetwork fetches the named CardanoNetwork. A NotFound result is
// translated to a typed not-found error so callers can show a friendly
// message; other errors are wrapped with namespace/name context.
func (a *Adapter) GetCardanoNetwork(ctx context.Context, namespace string, name string) (*yacdv1alpha1.CardanoNetwork, error) {
	network := &yacdv1alpha1.CardanoNetwork{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}
	if err := a.client.Get(ctx, key, network); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("cardanonetwork %s/%s not found", namespace, name)
		}
		return nil, fmt.Errorf("get cardanonetwork %s/%s: %w", namespace, name, err)
	}

	return network, nil
}

// GetSecretValue reads a single key from a Secret. The returned error
// distinguishes missing Secret from missing/empty key so callers can render
// a precise diagnostic.
func (a *Adapter) GetSecretValue(ctx context.Context, namespace string, name string, key string) (string, error) {
	secret := &corev1.Secret{}
	objectKey := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}
	if err := a.client.Get(ctx, objectKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("secret %s/%s not found", namespace, name)
		}
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}

	value, ok := secret.Data[key]
	if !ok || len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s is missing key %q", namespace, name, key)
	}

	return string(value), nil
}
