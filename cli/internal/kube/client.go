package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNotFound is the sentinel a Client returns (wrapped, with namespace/name
// context) when a requested object does not exist. Callers test for it with
// IsNotFound rather than reaching for the apimachinery error helpers, so the
// port stays the single source of not-found semantics.
var ErrNotFound = errors.New("not found")

// IsNotFound reports whether err indicates a requested object did not exist.
// It matches both the port's wrapped ErrNotFound and raw apimachinery
// NotFound status errors.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || apierrors.IsNotFound(err)
}

// Namespace ownership-stamp labels applied by EnsureNamespace, so a later
// teardown can recognise namespaces the CLI created.
const (
	// managedByLabel marks the namespace as YACD-managed.
	managedByLabel = "app.kubernetes.io/managed-by"

	// createdByLabel records that the CLI created the namespace.
	createdByLabel = "yacd.meigma.io/created-by"
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

	// DeleteCardanoNetwork deletes the named CardanoNetwork. It is idempotent:
	// a NotFound result returns nil so callers can treat "already gone" as
	// success.
	DeleteCardanoNetwork(ctx context.Context, namespace string, name string) error

	// ListCardanoNetworks lists CardanoNetworks in the given namespace, or
	// across all namespaces when namespace is empty.
	ListCardanoNetworks(ctx context.Context, namespace string) ([]yacdv1alpha1.CardanoNetwork, error)

	// EnsureNamespace creates the namespace if it does not exist and stamps it
	// with the CLI ownership labels. It is idempotent and safe to call on a
	// namespace the CLI did not create.
	EnsureNamespace(ctx context.Context, namespace string) error
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
			return nil, fmt.Errorf("cardanonetwork %s/%s %w", namespace, name, ErrNotFound)
		}
		return nil, fmt.Errorf("get cardanonetwork %s/%s: %w", namespace, name, err)
	}

	return network, nil
}

// DeleteCardanoNetwork deletes the named CardanoNetwork. A NotFound result is
// treated as success so `down` is idempotent; other errors are wrapped with
// namespace/name context.
func (a *Adapter) DeleteCardanoNetwork(ctx context.Context, namespace string, name string) error {
	network := &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	if err := a.client.Delete(ctx, network); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete cardanonetwork %s/%s: %w", namespace, name, err)
	}

	return nil
}

// ListCardanoNetworks lists CardanoNetworks in the given namespace. An empty
// namespace lists across all namespaces.
func (a *Adapter) ListCardanoNetworks(ctx context.Context, namespace string) ([]yacdv1alpha1.CardanoNetwork, error) {
	list := &yacdv1alpha1.CardanoNetworkList{}
	var opts []client.ListOption
	if strings.TrimSpace(namespace) != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	if err := a.client.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("list cardanonetworks: %w", err)
	}

	return list.Items, nil
}

// EnsureNamespace server-side-applies the namespace with the CLI ownership
// labels, creating it if absent. Apply is idempotent and only owns the labels
// it sets, so it neither errors on a pre-existing namespace nor clobbers
// labels owned by other field managers (for example a Pod Security label).
func (a *Adapter) EnsureNamespace(ctx context.Context, namespace string) error {
	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				managedByLabel: "yacd",
				createdByLabel: fieldOwner,
			},
		},
	}
	//nolint:staticcheck // client.Apply is still the practical SSA path for object structs without generated apply configurations, matching ApplyCardanoNetwork.
	if err := a.client.Patch(ctx, ns, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("ensure namespace %s: %w", namespace, err)
	}

	return nil
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
