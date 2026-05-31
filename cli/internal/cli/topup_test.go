package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testTopUpToken      = "super-secret-token-which-is-long-enough"
	testTopUpAuthSecret = "devnet-faucet-auth"
	testTopUpResponse   = `{"txId":"abc123","source":"utxo1","sourceAddress":"addr_test1source","destinationAddress":"addr_test1dest","lovelace":2000000}`
)

func TestTopUpReadsSecretAndPostsToFaucet(t *testing.T) {
	t.Parallel()

	type faucetRequest struct {
		path        string
		auth        string
		contentType string
		payload     topUpHTTPPayload
		err         error
	}
	requests := make(chan faucetRequest, 1)
	faucetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := faucetRequest{
			path:        r.URL.Path,
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
		}
		got.err = json.NewDecoder(r.Body).Decode(&got.payload)
		requests <- got
		if got.err != nil {
			http.Error(w, got.err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"txId":"abc123","source":"utxo2","sourceAddress":"addr_test1source","destinationAddress":"addr_test1dest","lovelace":2000000}`)
	}))
	t.Cleanup(faucetServer.Close)

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).
		Return(testTopUpToken, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--source", "utxo2", "--faucet-url", faucetServer.URL, "--json"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	got := <-requests
	require.NoError(t, got.err)
	assert.Equal(t, "/v1/topups", got.path)
	assert.Equal(t, "Bearer "+testTopUpToken, got.auth)
	assert.Equal(t, "application/json", got.contentType)
	assert.Equal(t, "addr_test1dest", got.payload.Address)
	assert.Equal(t, int64(2000000), got.payload.Lovelace)
	assert.Equal(t, "utxo2", got.payload.Source)
	for _, want := range []string{`"txId": "abc123"`, `"source": "utxo2"`, `"lovelace": 2000000`} {
		assert.Contains(t, stdout.String(), want)
	}
}

func TestTopUpUsesStatusEndpointByDefault(t *testing.T) {
	t.Parallel()

	faucetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, testTopUpResponse)
	}))
	t.Cleanup(faucetServer.Close)

	network := readyNetwork("devnet")
	network.Status.Endpoints.Faucet.URL = faucetServer.URL

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().
		GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).
		Return(testTopUpToken, nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000"})

	require.NoError(t, root.ExecuteContext(context.Background()))
}

func TestTopUpAllowsPublishedRemoteFaucetURLByDefault(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")
	network.Status.Endpoints.Faucet.URL = "http://devnet-faucet.devnet.svc.cluster.local:8080"

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().
		GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).
		Return(testTopUpToken, nil)

	httpMock := newHTTPMock(t)
	var capturedRequest *http.Request
	httpMock.EXPECT().Do(mock.Anything).
		Run(func(req *http.Request) { capturedRequest = req }).
		Return(successfulTopUpHTTPResponse(), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		HTTPClient:        httpMock,
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, capturedRequest)
	assert.Equal(t, "devnet-faucet.devnet.svc.cluster.local:8080", capturedRequest.URL.Host)
}

// TestTopUpRequiresTrustForRemoteCustomFaucetURLBeforeReadingSecret asserts
// the no-token-leak invariant: when --faucet-url points at a custom
// non-loopback host and --trust-faucet-url is absent, the trust gate must
// reject before GetSecretValue is ever called. The mock has no GetSecretValue
// expectation; mockery will fail the test if it is invoked.
func TestTopUpRequiresTrustForRemoteCustomFaucetURLBeforeReadingSecret(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--faucet-url", "https://faucet.example.com"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	for _, want := range []string{"devnet/devnet-faucet-auth", "faucet.example.com", "--trust-faucet-url"} {
		assert.Contains(t, err.Error(), want)
	}
	client.AssertNotCalled(t, "GetSecretValue", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTopUpAllowsTrustedRemoteHTTPSCustomFaucetURL(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).
		Return(testTopUpToken, nil)

	httpMock := newHTTPMock(t)
	var capturedRequest *http.Request
	httpMock.EXPECT().Do(mock.Anything).
		Run(func(req *http.Request) { capturedRequest = req }).
		Return(successfulTopUpHTTPResponse(), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		HTTPClient:        httpMock,
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--faucet-url", "https://faucet.example.com", "--trust-faucet-url"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, capturedRequest)
	assert.Equal(t, "faucet.example.com", capturedRequest.URL.Host)
	assert.Equal(t, "Bearer "+testTopUpToken, capturedRequest.Header.Get("Authorization"))
}

func TestTopUpRequiresAllowInsecureForTrustedRemoteHTTPCustomFaucetURL(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--faucet-url", "http://faucet.example.com", "--trust-faucet-url"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	for _, want := range []string{"devnet/devnet-faucet-auth", "faucet.example.com", "--allow-insecure-faucet-url"} {
		assert.Contains(t, err.Error(), want)
	}
	client.AssertNotCalled(t, "GetSecretValue", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTopUpAllowsTrustedRemoteHTTPCustomFaucetURLWithInsecureFlag(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).
		Return(testTopUpToken, nil)

	httpMock := newHTTPMock(t)
	var capturedRequest *http.Request
	httpMock.EXPECT().Do(mock.Anything).
		Run(func(req *http.Request) { capturedRequest = req }).
		Return(successfulTopUpHTTPResponse(), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		HTTPClient:        httpMock,
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--faucet-url", "http://faucet.example.com", "--trust-faucet-url", "--allow-insecure-faucet-url"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, capturedRequest)
	assert.Equal(t, "faucet.example.com", capturedRequest.URL.Host)
}

func TestTopUpReportsFaucetErrors(t *testing.T) {
	t.Parallel()

	faucetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
	}))
	t.Cleanup(faucetServer.Close)

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).
		Return(testTopUpToken, nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--faucet-url", faucetServer.URL})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401: unauthorized: bad token")
}

func TestTopUpRejectsStaleOrNotReadyStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*yacdv1alpha1.CardanoNetwork)
		wantErr string
	}{
		{
			name:    "stale status",
			mutate:  func(network *yacdv1alpha1.CardanoNetwork) { network.Status.ObservedGeneration = 0 },
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
		{
			name: "stale ready condition",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				for i := range network.Status.Conditions {
					if network.Status.Conditions[i].Type == "Ready" {
						network.Status.Conditions[i].ObservedGeneration = 0
					}
				}
			},
			wantErr: "Ready condition is missing or stale",
		},
		{
			name: "degraded",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Status.Conditions = append(network.Status.Conditions, metav1.Condition{
					Type:               "Degraded",
					Status:             metav1.ConditionTrue,
					Reason:             "UnsupportedSpec",
					Message:            "bad faucet config",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				})
			},
			wantErr: "is degraded: UnsupportedSpec: bad faucet config",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			network := readyNetwork("devnet")
			tc.mutate(network)

			client := newKubeMock(t)
			client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
			client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)

			root := NewRootCommand(Options{
				Viper:             viper.New(),
				KubeClientFactory: kubeClientFactory(client),
			})
			root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000"})

			err := root.ExecuteContext(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// successfulTopUpHTTPResponse builds a fresh 200 OK response for the
// HTTP transport mock. Each call must yield a fresh body because the
// transport drains it.
func successfulTopUpHTTPResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(testTopUpResponse)),
	}
}
