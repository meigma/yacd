package cli

import (
	"os"
	"path/filepath"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// testDevConfig is the canonical developer-environment YAML used across the
// command tests. Identity (name/namespace) is supplied on the command line,
// not in the file, so the document carries only the apiVersion/kind envelope
// and the network spec; it exercises the storage size requirement and a
// one-pool localnet.
const testDevConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: local
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 2Gi
    local:
      networkMagic: 42
      era: conway
      timing:
        slotLength: 100ms
        epochLength: 500
      topology:
        pools:
          count: 1
`

const testPublicMainnetDevConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
    public:
      profile: mainnet
      bootstrap:
        mithril: {}
`

const testPublicPreviewDevConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 20Gi
    public:
      profile: preview
`

const testPublicCustomDevConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 20Gi
    public:
      profile: custom
      configSource:
        configMapRef:
          name: custom-profile
`

// writeTempConfig writes contents to a yacd.yaml file inside a fresh
// t.TempDir and returns the absolute path.
func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "yacd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600), "write config")

	return path
}

// kubeClientFactory wraps a mock client in the KubeClientFactory signature.
func kubeClientFactory(client kube.Client) KubeClientFactory {
	return func(kube.Config) (kube.Client, error) {
		return client, nil
	}
}

// newKubeMock returns a mocks.Client that auto-asserts at test cleanup.
func newKubeMock(t *testing.T) *mocks.Client {
	t.Helper()
	return mocks.NewClient(t)
}

// newHTTPMock returns a mocks.HTTPDoer that auto-asserts at test cleanup.
func newHTTPMock(t *testing.T) *mocks.HTTPDoer {
	t.Helper()
	return mocks.NewHTTPDoer(t)
}

// readyNetwork builds a CardanoNetwork in a Ready / FaucetReady state with
// the published Ogmios/Kupo/Faucet endpoints and faucet auth Secret name.
// Tests that need a different shape mutate the returned object.
func readyNetwork(namespace string) *yacdv1alpha1.CardanoNetwork {
	networkMagic := int64(42)
	era := yacdv1alpha1.CardanoEraConway
	name := "devnet"

	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Status: yacdv1alpha1.CardanoNetworkStatus{
			ObservedGeneration: 1,
			Network: &yacdv1alpha1.CardanoNetworkIdentityStatus{
				Mode:         yacdv1alpha1.CardanoNetworkModeLocal,
				NetworkMagic: &networkMagic,
				Era:          &era,
			},
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-ogmios",
					Port:        1337,
					URL:         "ws://" + name + "-ogmios." + namespace + ".svc.cluster.local:1337",
				},
				Kupo: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-kupo",
					Port:        1442,
					URL:         "http://" + name + "-kupo." + namespace + ".svc.cluster.local:1442",
				},
				Faucet: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-faucet",
					Port:        8080,
					URL:         "http://" + name + "-faucet." + namespace + ".svc.cluster.local:8080",
				},
			},
			Faucet: &yacdv1alpha1.FaucetStatus{
				AuthSecretName: name + "-faucet-auth",
			},
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Ready",
					Message:            "ready",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               "FaucetReady",
					Status:             metav1.ConditionTrue,
					Reason:             "FaucetReady",
					Message:            "ready",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}
}
