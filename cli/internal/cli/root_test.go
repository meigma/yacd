package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testDevConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
metadata:
  name: devnet
  namespace: config-ns
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

func TestVersionFlagPrintsBuildMetadata(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out: &stdout,
		Err: &stderr,
		Build: BuildInfo{
			Version: "0.1.0",
			Commit:  "abc1234",
			Date:    "2026-05-22T10:00:00Z",
		},
	})
	root.SetArgs([]string{"--version"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if got, want := stdout.String(), "yacd 0.1.0 (abc1234) built 2026-05-22T10:00:00Z\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestDeployDryRunPrintsManifestWithoutKubeClient(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed for dry run")
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--namespace", "flag-ns", "--dry-run"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}

	for _, want := range []string{
		"apiVersion: yacd.meigma.io/v1alpha1",
		"kind: CardanoNetwork",
		"name: devnet",
		"namespace: flag-ns",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if got := stderr.String(); !strings.Contains(got, "Dry run: rendered CardanoNetwork flag-ns/devnet; no resources applied.") {
		t.Fatalf("stderr = %q, want dry-run status", got)
	}
}

func TestDeployDryRunUsesKubeDefaultNamespace(t *testing.T) {
	t.Setenv("YACD_NAMESPACE", "")

	configPath := writeTempConfig(t, strings.Replace(testDevConfig, "  namespace: config-ns\n", "", 1))
	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed for dry run")
		},
		KubeNamespaceResolver: func(kube.Config) (string, error) {
			return "kube-ns", nil
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--dry-run"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "namespace: kube-ns") {
		t.Fatalf("stdout = %s, want kube namespace", got)
	}
}

func TestInfoReadsGlobalKubeEnvironment(t *testing.T) {
	t.Setenv("YACD_KUBECONFIG", "/tmp/yacd-kubeconfig")
	t.Setenv("YACD_KUBE_CONTEXT", "dev-context")
	t.Setenv("YACD_NAMESPACE", "env-ns")

	var capturedConfig kube.Config
	var requestedNamespace string
	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Viper: viper.New(),
		KubeClientFactory: func(config kube.Config) (kube.Client, error) {
			capturedConfig = config
			return &fakeKubeClient{
				defaultNamespace:   "default-ns",
				network:            readyNetwork("env-ns", "devnet"),
				requestedNamespace: &requestedNamespace,
			}, nil
		},
	})
	root.SetArgs([]string{"info", "devnet", "--json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if got, want := capturedConfig.Context, "dev-context"; got != want {
		t.Fatalf("context = %q, want %q", got, want)
	}
	if got, want := capturedConfig.Kubeconfig, "/tmp/yacd-kubeconfig"; got != want {
		t.Fatalf("kubeconfig = %q, want %q", got, want)
	}
	if got, want := requestedNamespace, "env-ns"; got != want {
		t.Fatalf("requested namespace = %q, want %q", got, want)
	}
	for _, want := range []string{
		`"name": "devnet"`,
		`"namespace": "env-ns"`,
		`"type": "Ready"`,
		`"url": "ws://devnet-ogmios.env-ns.svc.cluster.local:1337"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestDeployAppliesAndWaits(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stderr bytes.Buffer
	fakeClient := &fakeKubeClient{
		defaultNamespace: "default-ns",
		network:          readyNetwork("config-ns", "devnet"),
	}
	root := NewRootCommand(Options{
		Err:   &stderr,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return fakeClient, nil
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--wait"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if fakeClient.applied == nil {
		t.Fatal("CardanoNetwork was not applied")
	}
	if got, want := fakeClient.applied.Namespace, "config-ns"; got != want {
		t.Fatalf("applied namespace = %q, want %q", got, want)
	}
	if got := stderr.String(); !strings.Contains(got, "CardanoNetwork config-ns/devnet is ready.") {
		t.Fatalf("stderr = %q, want ready status", got)
	}
}

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "yacd.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

type fakeKubeClient struct {
	defaultNamespace   string
	network            *yacdv1alpha1.CardanoNetwork
	applied            *yacdv1alpha1.CardanoNetwork
	requestedNamespace *string
}

func (f *fakeKubeClient) DefaultNamespace() string {
	return f.defaultNamespace
}

func (f *fakeKubeClient) ApplyCardanoNetwork(_ context.Context, network *yacdv1alpha1.CardanoNetwork) error {
	f.applied = network.DeepCopy()
	return nil
}

func (f *fakeKubeClient) GetCardanoNetwork(_ context.Context, namespace string, _ string) (*yacdv1alpha1.CardanoNetwork, error) {
	if f.requestedNamespace != nil {
		*f.requestedNamespace = namespace
	}
	return f.network.DeepCopy(), nil
}

func readyNetwork(namespace string, name string) *yacdv1alpha1.CardanoNetwork {
	networkMagic := int64(42)
	era := yacdv1alpha1.CardanoEraConway

	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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
			},
		},
	}
}
