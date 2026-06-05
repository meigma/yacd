package cardanonetwork

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanowallet "github.com/meigma/yacd/internal/cardano/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveFaucetWalletSettings(t *testing.T) {
	localPlan := primaryNetworkPlan{Mode: yacdv1alpha1.CardanoNetworkModeLocal}
	publicPlan := primaryNetworkPlan{Mode: yacdv1alpha1.CardanoNetworkModePublic}

	t.Run("disabled on non-local networks", func(t *testing.T) {
		network := localCardanoNetwork("fw")
		settings := resolveFaucetWalletSettings(network, publicPlan)
		assert.False(t, settings.enabled)
	})

	t.Run("enabled on a local network", func(t *testing.T) {
		network := localCardanoNetwork("fw")
		settings := resolveFaucetWalletSettings(network, localPlan)
		assert.True(t, settings.enabled)
		assert.Equal(t, defaultFaucetWalletFundingLovelace, settings.fundingLovelace)
		assert.Equal(t, primaryFaucetWalletSecretName(network), settings.secretName)
	})
}

func TestFaucetWalletEnabledPredicate(t *testing.T) {
	t.Run("local network", func(t *testing.T) {
		assert.True(t, faucetWalletEnabled(localCardanoNetwork("fw")))
	})

	t.Run("public network", func(t *testing.T) {
		assert.False(t, faucetWalletEnabled(publicPreviewCardanoNetwork("fw")))
	})
}

func TestApplyPrimaryFaucetWalletSecretCreatesOnceAndPreservesKeys(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("faucet-wallet-create")

	reconciler := newTestReconciler(t, network)
	desired, err := (primaryWorkloadBuilder{scheme: reconciler.Scheme}).faucetWalletSecret(network, faucetWalletSettings{secretName: primaryFaucetWalletSecretName(network)})
	require.NoError(t, err)

	op, created, err := reconciler.applyPrimaryFaucetWalletSecret(ctx, desired)
	require.NoError(t, err)
	assert.Equal(t, "created", string(op))

	// The marker labels identify the well-known, genesis-funded faucet wallet.
	assert.Equal(t, faucetWalletName, created.Labels[walletNameLabel])
	assert.Equal(t, walletSourceGenesisFunded, created.Labels[walletSourceLabel])

	address := string(created.Data[walletAddressKey])
	require.NotEmpty(t, address)
	assert.NotEmpty(t, created.Data[walletSigningKeyKey])
	assert.NotEmpty(t, created.Data[walletVerificationKeyKey])

	// The generated key material is valid: the verification envelope derives the
	// published address.
	derived := addressFromVerificationEnvelope(t, created.Data[walletVerificationKeyKey])
	assert.Equal(t, address, derived)

	// A second apply must preserve the existing key material verbatim — never
	// regenerate the genesis-funded faucet wallet, or the funded address would be
	// stranded.
	op, again, err := reconciler.applyPrimaryFaucetWalletSecret(ctx, desired.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, "unchanged", string(op))
	assert.Equal(t, address, string(again.Data[walletAddressKey]))
	assert.Equal(t, created.Data[walletSigningKeyKey], again.Data[walletSigningKeyKey])
}

func TestEnsurePrimaryFaucetWalletSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled returns no address", func(t *testing.T) {
		// The faucet wallet is gated on local mode; a public network never
		// bootstraps one.
		network := publicPreviewCardanoNetwork("fw-off")
		reconciler := newTestReconciler(t, network)

		result, err := reconciler.ensurePrimaryFaucetWalletSecret(ctx, network)
		require.NoError(t, err)
		assert.False(t, result.enabled)
		assert.Empty(t, result.address)
		assert.Nil(t, result.object)
	})

	t.Run("creates the Secret once and returns a stable address", func(t *testing.T) {
		network := localCardanoNetwork("fw-on")
		reconciler := newTestReconciler(t, network)

		first, err := reconciler.ensurePrimaryFaucetWalletSecret(ctx, network)
		require.NoError(t, err)
		assert.True(t, first.enabled)
		require.True(t, strings.HasPrefix(first.address, "addr_test1"))
		assert.Equal(t, "created", string(first.operation))

		second, err := reconciler.ensurePrimaryFaucetWalletSecret(ctx, network)
		require.NoError(t, err)
		assert.Equal(t, first.address, second.address, "the faucet wallet address must be stable across reconciles")
		assert.Equal(t, "unchanged", string(second.operation))
	})
}

func addressFromVerificationEnvelope(t *testing.T, envelope []byte) string {
	t.Helper()
	// The envelope's cborHex is "5820" (CBOR header for a 32-byte byte string)
	// followed by the verification key hex.
	const cborPrefix = "5820"
	var decoded struct {
		CBORHex string `json:"cborHex"`
	}
	require.NoError(t, json.Unmarshal(envelope, &decoded))
	require.True(t, strings.HasPrefix(decoded.CBORHex, cborPrefix))
	keyBytes, err := hex.DecodeString(decoded.CBORHex[len(cborPrefix):])
	require.NoError(t, err)
	address, err := cardanowallet.DeriveTestnetAddress(keyBytes)
	require.NoError(t, err)

	return address
}
