package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestDeployRejectsUnexpectedArgs(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid args")
		},
	})
	root.SetArgs([]string{"deploy", "unexpected", "-f", configPath, "--dry-run"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("ExecuteContext succeeded, want argument error")
	}
	if got := err.Error(); !strings.Contains(got, `unknown command "unexpected"`) {
		t.Fatalf("error = %q, want unexpected arg message", got)
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

func TestDeployRejectsInvalidWaitTimeoutBeforeApply(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid timeout")
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--wait", "--timeout", "0s"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("ExecuteContext succeeded, want timeout error")
	}
	if got := err.Error(); !strings.Contains(got, "--timeout must be greater than 0 when --wait is set") {
		t.Fatalf("error = %q, want timeout validation", got)
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
				network:            readyNetwork("env-ns"),
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
		`"url": "http://devnet-kupo.env-ns.svc.cluster.local:1442"`,
		`"url": "http://devnet-faucet.env-ns.svc.cluster.local:8080"`,
		`"authSecretName": "devnet-faucet-auth"`,
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
		network:          readyNetwork("config-ns"),
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

func TestTopUpReadsSecretAndPostsToFaucet(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotContentType string
	var gotPayload topUpHTTPPayload
	faucetServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/topups" {
			t.Fatalf("path = %q, want /v1/topups", request.URL.Path)
		}
		gotAuth = request.Header.Get("Authorization")
		gotContentType = request.Header.Get("Content-Type")
		if err := json.NewDecoder(request.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"txId":"abc123","source":"utxo2","sourceAddress":"addr_test1source","destinationAddress":"addr_test1dest","lovelace":2000000}`)
	}))
	t.Cleanup(faucetServer.Close)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return &fakeKubeClient{
				defaultNamespace: "default-ns",
				network:          readyNetwork("default-ns"),
				secretValues: map[string]string{
					"default-ns/devnet-faucet-auth/token": "super-secret-token-which-is-long-enough",
				},
			}, nil
		},
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--source", "utxo2", "--faucet-url", faucetServer.URL, "--json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if got, want := gotAuth, "Bearer super-secret-token-which-is-long-enough"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := gotContentType, "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if gotPayload.Address != "addr_test1dest" || gotPayload.Lovelace != 2000000 || gotPayload.Source != "utxo2" {
		t.Fatalf("payload = %+v, want address/source/lovelace", gotPayload)
	}
	for _, want := range []string{`"txId": "abc123"`, `"source": "utxo2"`, `"lovelace": 2000000`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestTopUpUsesStatusEndpointByDefault(t *testing.T) {
	t.Parallel()

	faucetServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"txId":"abc123","source":"utxo1","sourceAddress":"addr_test1source","destinationAddress":"addr_test1dest","lovelace":2000000}`)
	}))
	t.Cleanup(faucetServer.Close)

	network := readyNetwork("default-ns")
	network.Status.Endpoints.Faucet.URL = faucetServer.URL
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return &fakeKubeClient{
				defaultNamespace: "default-ns",
				network:          network,
				secretValues: map[string]string{
					"default-ns/devnet-faucet-auth/token": "super-secret-token-which-is-long-enough",
				},
			}, nil
		},
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
}

func TestTopUpReportsFaucetErrors(t *testing.T) {
	t.Parallel()

	faucetServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(writer, `{"error":{"code":"unauthorized","message":"bad token"}}`)
	}))
	t.Cleanup(faucetServer.Close)

	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return &fakeKubeClient{
				defaultNamespace: "default-ns",
				network:          readyNetwork("default-ns"),
				secretValues: map[string]string{
					"default-ns/devnet-faucet-auth/token": "super-secret-token-which-is-long-enough",
				},
			}, nil
		},
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--faucet-url", faucetServer.URL})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("ExecuteContext succeeded, want error")
	}
	if got := err.Error(); !strings.Contains(got, "HTTP 401: unauthorized: bad token") {
		t.Fatalf("error = %q, want faucet error", got)
	}
}

func TestTopUpRejectsStaleOrNotReadyStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*yacdv1alpha1.CardanoNetwork)
		wantErr string
	}{
		{
			name: "stale status",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Status.ObservedGeneration = 0
			},
			wantErr: "status is stale",
		},
		{
			name: "faucet not ready",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				for i := range network.Status.Conditions {
					if network.Status.Conditions[i].Type == "FaucetReady" {
						network.Status.Conditions[i].Status = metav1.ConditionFalse
					}
				}
			},
			wantErr: "is not faucet-ready",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			network := readyNetwork("default-ns")
			tt.mutate(network)
			root := NewRootCommand(Options{
				Viper: viper.New(),
				KubeClientFactory: func(kube.Config) (kube.Client, error) {
					return &fakeKubeClient{
						defaultNamespace: "default-ns",
						network:          network,
					}, nil
				},
			})
			root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000"})

			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("ExecuteContext succeeded, want readiness error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
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
	secretValues       map[string]string
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

func (f *fakeKubeClient) GetSecretValue(_ context.Context, namespace string, name string, key string) (string, error) {
	value, ok := f.secretValues[namespace+"/"+name+"/"+key]
	if !ok {
		return "", fmt.Errorf("secret value not found")
	}
	return value, nil
}

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
