package kube

import (
	"context"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultNamespace = "default"
	fieldOwner       = "yacd-cli"
)

// Config controls Kubernetes client construction.
type Config struct {
	Kubeconfig string
	Context    string
}

// Client is the Kubernetes behavior used by the CLI command layer.
type Client interface {
	DefaultNamespace() string
	ApplyCardanoNetwork(ctx context.Context, network *yacdv1alpha1.CardanoNetwork) error
	GetCardanoNetwork(ctx context.Context, namespace string, name string) (*yacdv1alpha1.CardanoNetwork, error)
}

type runtimeClient struct {
	client    client.Client
	namespace string
}

// NewClient constructs a controller-runtime client from the user's kubeconfig.
func NewClient(config Config) (Client, error) {
	restConfig, namespace, err := RESTConfig(config)
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yacdv1alpha1.AddToScheme(scheme))

	kubeClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	return &runtimeClient{
		client:    kubeClient,
		namespace: namespace,
	}, nil
}

// RESTConfig resolves the user's kubeconfig and default namespace.
func RESTConfig(config Config) (*rest.Config, string, error) {
	clientConfig := newClientConfig(config)

	namespace, err := DefaultNamespace(config)
	if err != nil {
		return nil, "", err
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("load Kubernetes config: %w", err)
	}

	return restConfig, namespace, nil
}

// DefaultNamespace resolves the namespace selected by the user's kubeconfig.
func DefaultNamespace(config Config) (string, error) {
	clientConfig := newClientConfig(config)

	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return "", fmt.Errorf("resolve Kubernetes namespace: %w", err)
	}
	if strings.TrimSpace(namespace) == "" {
		namespace = defaultNamespace
	}

	return namespace, nil
}

func newClientConfig(config Config) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if strings.TrimSpace(config.Kubeconfig) != "" {
		loadingRules.ExplicitPath = strings.TrimSpace(config.Kubeconfig)
	}

	overrides := &clientcmd.ConfigOverrides{
		CurrentContext: strings.TrimSpace(config.Context),
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}

func (c *runtimeClient) DefaultNamespace() string {
	if strings.TrimSpace(c.namespace) == "" {
		return defaultNamespace
	}

	return c.namespace
}

func (c *runtimeClient) ApplyCardanoNetwork(ctx context.Context, network *yacdv1alpha1.CardanoNetwork) error {
	if network == nil {
		return fmt.Errorf("cardanonetwork is required")
	}
	//nolint:staticcheck // client.Apply is still the practical SSA path for CRD object structs without generated apply configurations.
	if err := c.client.Patch(ctx, network.DeepCopy(), client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply cardanonetwork %s/%s: %w", network.Namespace, network.Name, err)
	}

	return nil
}

func (c *runtimeClient) GetCardanoNetwork(ctx context.Context, namespace string, name string) (*yacdv1alpha1.CardanoNetwork, error) {
	network := &yacdv1alpha1.CardanoNetwork{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}
	if err := c.client.Get(ctx, key, network); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("cardanonetwork %s/%s not found", namespace, name)
		}
		return nil, fmt.Errorf("get cardanonetwork %s/%s: %w", namespace, name, err)
	}

	return network, nil
}
