package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAwaitConfirmationSucceedsWhenTxAppears(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"other", "abc123"}, nil)

	require.NoError(t, awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 5*time.Second))
}

func TestAwaitConfirmationMatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"ABC123"}, nil)

	require.NoError(t, awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 5*time.Second))
}

func TestAwaitConfirmationTimesOutWithoutMatch(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"unrelated"}, nil)

	err := awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 50*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not confirmed within")
}

func TestAwaitConfirmationSurfacesLastQueryErrorOnTimeout(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return(nil, errors.New("kupo unreachable"))

	err := awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 50*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kupo unreachable")
}

func TestTopUpAwaitRequiresKupoURL(t *testing.T) {
	t.Parallel()

	// --await without a Kupo URL must fail before any cluster contact: the
	// mock has no expectations, so any client call would fail the test.
	client := newKubeMock(t)

	var stderr bytes.Buffer
	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"topup", "devnet", "--address", "addr_test1dest", "--lovelace", "2000000", "--await"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--await requires a Kupo URL")
}

func TestTopUpAwaitRejectsMalformedKupoURLBeforeClusterContact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kupoURL string
	}{
		{name: "relative", kupoURL: "kupo.local:1442"},
		{name: "missing host", kupoURL: "http://"},
		{name: "unsupported scheme", kupoURL: "ws://127.0.0.1:1442"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock has no expectations. Any cluster contact would fail the
			// test, which also proves no faucet POST can be attempted.
			client := newKubeMock(t)

			root := NewRootCommand(Options{
				Err:               &bytes.Buffer{},
				Viper:             viper.New(),
				KubeClientFactory: kubeClientFactory(client),
			})
			root.SetArgs([]string{
				"topup", "devnet",
				"--address", "addr_test1dest", "--lovelace", "2000000",
				"--await", "--kupo-url", tc.kupoURL,
			})

			err := root.ExecuteContext(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--kupo-url")
		})
	}
}

func TestTopUpAwaitConfirmsOnChain(t *testing.T) {
	t.Parallel()

	faucetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, testTopUpResponse) // txId abc123, destination addr_test1dest
	}))
	t.Cleanup(faucetServer.Close)

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).Return(testTopUpToken, nil)

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"abc123"}, nil)
	var gotKupoURL string

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
		UTxOConfirmerFactory: func(kupoURL string) UTxOConfirmer {
			gotKupoURL = kupoURL

			return confirmer
		},
	})
	root.SetArgs([]string{
		"topup", "devnet",
		"--address", "addr_test1dest", "--lovelace", "2000000",
		"--faucet-url", faucetServer.URL,
		"--await", "--kupo-url", "http://127.0.0.1:1442",
	})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "http://127.0.0.1:1442", gotKupoURL)
	assert.Contains(t, stdout.String(), "Submitted top-up abc123")
	assert.Contains(t, stdout.String(), "Confirmed on-chain.")
	// The poll is otherwise silent, so it announces the wait on stderr (keeping
	// stdout/--json clean).
	assert.Contains(t, stderr.String(), "Waiting up to")
	assert.Contains(t, stderr.String(), "abc123")
}

// TestTopUpAwaitQueriesRequestedAddressNotEcho pins the security invariant that
// --await polls the address we asked to fund, not the faucet's echoed value:
// the faucet here echoes a different destination, and the confirmer must still
// be queried with the requested --address. A regression to result.Destination-
// Address would query the echoed value and fail the mock's expectation.
func TestTopUpAwaitQueriesRequestedAddressNotEcho(t *testing.T) {
	t.Parallel()

	faucetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Echoes a destination that differs from the requested --address.
		_, _ = fmt.Fprint(w, `{"txId":"abc123","source":"utxo1","sourceAddress":"addr_test1source","destinationAddress":"addr_test1echoed","lovelace":2000000}`)
	}))
	t.Cleanup(faucetServer.Close)

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).Return(testTopUpToken, nil)

	confirmer := mocks.NewUTxOConfirmer(t)
	// The invariant: queried with the requested address, never the echoed one.
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"abc123"}, nil)

	root := NewRootCommand(Options{
		Out:                  &bytes.Buffer{},
		Err:                  &bytes.Buffer{},
		Viper:                viper.New(),
		KubeClientFactory:    kubeClientFactory(client),
		UTxOConfirmerFactory: func(string) UTxOConfirmer { return confirmer },
	})
	root.SetArgs([]string{
		"topup", "devnet",
		"--address", "addr_test1dest", "--lovelace", "2000000",
		"--faucet-url", faucetServer.URL,
		"--await", "--kupo-url", "http://127.0.0.1:1442",
	})

	require.NoError(t, root.ExecuteContext(context.Background()))
}

func TestTopUpAwaitReadsKupoURLFromEnv(t *testing.T) {
	// No t.Parallel: t.Setenv is incompatible with parallel tests.
	t.Setenv("YACD_KUPO_URL", "http://kupo-from-env:1442")

	faucetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, testTopUpResponse)
	}))
	t.Cleanup(faucetServer.Close)

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("devnet").Maybe()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", testTopUpAuthSecret, faucetAuthTokenKey).Return(testTopUpToken, nil)

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"abc123"}, nil)
	var gotKupoURL string

	root := NewRootCommand(Options{
		Out:               &bytes.Buffer{},
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
		UTxOConfirmerFactory: func(kupoURL string) UTxOConfirmer {
			gotKupoURL = kupoURL

			return confirmer
		},
	})
	root.SetArgs([]string{
		"topup", "devnet",
		"--address", "addr_test1dest", "--lovelace", "2000000",
		"--faucet-url", faucetServer.URL, "--await",
	})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "http://kupo-from-env:1442", gotKupoURL, "YACD_KUPO_URL must satisfy --await via viper AutomaticEnv")
}
