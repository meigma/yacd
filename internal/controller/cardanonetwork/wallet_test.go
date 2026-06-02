package cardanonetwork

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanowallet "github.com/meigma/yacd/internal/cardano/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func enableWallet(network *yacdv1alpha1.CardanoNetwork, fundingLovelace int64) {
	if network.Spec.ChainAPI == nil {
		network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{}
	}
	network.Spec.ChainAPI.Wallet = &yacdv1alpha1.WalletSpec{
		Enabled:         true,
		FundingLovelace: fundingLovelace,
	}
}

func TestResolveWalletSettings(t *testing.T) {
	localPlan := primaryNetworkPlan{Mode: yacdv1alpha1.CardanoNetworkModeLocal}
	enabledFaucet := faucetSettings{enabled: true, maxTopUpLovelace: defaultFaucetMaxLovelace}
	enabledKupo := kupoSettings{enabled: true}

	t.Run("disabled when not requested", func(t *testing.T) {
		network := localCardanoNetwork("w")
		settings, err := resolveWalletSettings(network, localPlan, enabledFaucet, enabledKupo)
		require.NoError(t, err)
		assert.False(t, settings.enabled)
	})

	t.Run("enabled requires faucet", func(t *testing.T) {
		network := localCardanoNetwork("w")
		enableWallet(network, defaultFaucetMaxLovelace)
		_, err := resolveWalletSettings(network, localPlan, faucetSettings{enabled: false}, enabledKupo)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires the faucet")
	})

	t.Run("enabled requires kupo", func(t *testing.T) {
		network := localCardanoNetwork("w")
		enableWallet(network, defaultFaucetMaxLovelace)
		_, err := resolveWalletSettings(network, localPlan, enabledFaucet, kupoSettings{enabled: false})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires kupo")
	})

	t.Run("enabled rejects non-local", func(t *testing.T) {
		network := localCardanoNetwork("w")
		enableWallet(network, defaultFaucetMaxLovelace)
		_, err := resolveWalletSettings(network, primaryNetworkPlan{Mode: yacdv1alpha1.CardanoNetworkModePublic}, enabledFaucet, enabledKupo)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only for local")
	})

	t.Run("funding must fit faucet max", func(t *testing.T) {
		network := localCardanoNetwork("w")
		enableWallet(network, defaultFaucetMaxLovelace+1)
		_, err := resolveWalletSettings(network, localPlan, enabledFaucet, enabledKupo)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds faucet")
	})

	t.Run("enabled resolves", func(t *testing.T) {
		network := localCardanoNetwork("w")
		enableWallet(network, 5_000_000_000)
		settings, err := resolveWalletSettings(network, localPlan, enabledFaucet, enabledKupo)
		require.NoError(t, err)
		assert.True(t, settings.enabled)
		assert.Equal(t, int64(5_000_000_000), settings.fundingLovelace)
		assert.Equal(t, primaryWalletSecretName(network), settings.secretName)
	})
}

func TestApplyPrimaryWalletSecretCreatesOnceAndPreservesKeys(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("wallet-create")
	enableFaucet(network)
	enableWallet(network, defaultFaucetMaxLovelace)

	reconciler := newTestReconciler(t, network)
	desired, err := (primaryWorkloadBuilder{scheme: reconciler.Scheme}).walletSecret(network, walletSettings{secretName: primaryWalletSecretName(network)})
	require.NoError(t, err)

	op, created, err := reconciler.applyPrimaryWalletSecret(ctx, desired)
	require.NoError(t, err)
	assert.Equal(t, "created", string(op))

	address := string(created.Data[walletAddressKey])
	require.NotEmpty(t, address)
	assert.NotEmpty(t, created.Data[walletSigningKeyKey])
	assert.NotEmpty(t, created.Data[walletVerificationKeyKey])

	// The generated key material is valid: the verification envelope derives the
	// published address.
	derived := addressFromVerificationEnvelope(t, created.Data[walletVerificationKeyKey])
	assert.Equal(t, address, derived)

	// A second apply must preserve the existing key material verbatim — never
	// regenerate the developer's funded wallet.
	op, again, err := reconciler.applyPrimaryWalletSecret(ctx, desired.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, "unchanged", string(op))
	assert.Equal(t, address, string(again.Data[walletAddressKey]))
	assert.Equal(t, created.Data[walletSigningKeyKey], again.Data[walletSigningKeyKey])
}

func TestPrimaryWalletReadyConditionFundingLifecycle(t *testing.T) {
	ctx := context.Background()
	conditionTrue := func(c conditionReason) metav1.Condition {
		return metav1.Condition{Status: metav1.ConditionTrue, Reason: string(c)}
	}
	conditionFalse := metav1.Condition{Status: metav1.ConditionFalse}

	const funding int64 = 100_000_000_000

	setup := func(t *testing.T, confirmer walletConfirmer, funder walletFunder, statusTxID string) (*CardanoNetworkReconciler, *yacdv1alpha1.CardanoNetwork, walletSettings, *corev1.Service, *corev1.Service) {
		t.Helper()
		network := localCardanoNetwork("wallet-fund")
		enableFaucet(network)
		enableWallet(network, funding)
		if statusTxID != "" {
			network.Status.Wallet = &yacdv1alpha1.WalletStatus{FundedTxID: statusTxID}
		}

		walletMaterial, err := cardanowallet.New()
		require.NoError(t, err)
		walletSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: network.Namespace, Name: primaryWalletSecretName(network)},
			Data: map[string][]byte{
				walletAddressKey:    []byte(walletMaterial.Address),
				walletSigningKeyKey: walletMaterial.SigningKeyEnvelope,
			},
		}
		faucetAuthSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: network.Namespace, Name: primaryFaucetAuthSecretName(network)},
			Data:       map[string][]byte{faucetAuthTokenKey: []byte("0123456789abcdef0123456789abcdef0123456789")},
		}

		reconciler := newTestReconciler(t, network, walletSecret, faucetAuthSecret)
		reconciler.walletConfirmerOverride = confirmer
		reconciler.walletFunderOverride = funder

		kupoService := serviceWithPort(network.Namespace, primaryKupoServiceName(network), defaultKupoPort)
		faucetService := serviceWithPort(network.Namespace, primaryFaucetServiceName(network), defaultFaucetPort)

		return reconciler, network, walletSettings{enabled: true, fundingLovelace: funding, secretName: primaryWalletSecretName(network)}, kupoService, faucetService
	}

	t.Run("disabled", func(t *testing.T) {
		reconciler := newTestReconciler(t)
		condition, status, degraded, err := reconciler.primaryWalletReadyCondition(ctx, localCardanoNetwork("w"), walletSettings{enabled: false}, conditionTrue(conditionReasonFaucetReady), conditionTrue(conditionReasonKupoReady), nil, nil)
		require.NoError(t, err)
		assert.Equal(t, metav1.ConditionFalse, condition.Status)
		assert.Equal(t, string(conditionReasonWalletDisabled), condition.Reason)
		assert.Nil(t, status)
		assert.False(t, degraded)
	})

	t.Run("waits for faucet and kupo", func(t *testing.T) {
		funderCalls := 0
		reconciler, network, settings, kupoSvc, faucetSvc := setup(t,
			walletConfirmerFunc(func(context.Context, string, string, int64) (bool, error) { return false, nil }),
			walletFunderFunc(func(context.Context, string, string, string, int64) (string, error) { funderCalls++; return "", nil }), "")
		condition, status, degraded, err := reconciler.primaryWalletReadyCondition(ctx, network, settings, conditionFalse, conditionTrue(conditionReasonKupoReady), kupoSvc, faucetSvc)
		require.NoError(t, err)
		assert.Equal(t, string(conditionReasonWalletFundingPending), condition.Reason)
		assert.False(t, degraded)
		assert.Equal(t, 0, funderCalls, "must not fund before dependencies are ready")
		require.NotNil(t, status)
		assert.NotEmpty(t, status.Address)
	})

	t.Run("confirmed funding is ready", func(t *testing.T) {
		reconciler, network, settings, kupoSvc, faucetSvc := setup(t,
			walletConfirmerFunc(func(_ context.Context, _ string, _ string, minLovelace int64) (bool, error) {
				assert.Equal(t, funding, minLovelace)
				return true, nil
			}),
			walletFunderFunc(func(context.Context, string, string, string, int64) (string, error) {
				t.Fatal("funder must not be called when already funded")
				return "", nil
			}), "")
		condition, status, degraded, err := reconciler.primaryWalletReadyCondition(ctx, network, settings, conditionTrue(conditionReasonFaucetReady), conditionTrue(conditionReasonKupoReady), kupoSvc, faucetSvc)
		require.NoError(t, err)
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
		assert.Equal(t, string(conditionReasonWalletReady), condition.Reason)
		require.NotNil(t, status)
		assert.True(t, status.Funded)
		assert.False(t, degraded)
	})

	t.Run("unfunded submits once and records txid", func(t *testing.T) {
		funderCalls := 0
		reconciler, network, settings, kupoSvc, faucetSvc := setup(t,
			walletConfirmerFunc(func(context.Context, string, string, int64) (bool, error) { return false, nil }),
			walletFunderFunc(func(context.Context, string, string, string, int64) (string, error) {
				funderCalls++
				return "tx-abc", nil
			}), "")
		condition, status, degraded, err := reconciler.primaryWalletReadyCondition(ctx, network, settings, conditionTrue(conditionReasonFaucetReady), conditionTrue(conditionReasonKupoReady), kupoSvc, faucetSvc)
		require.NoError(t, err)
		assert.Equal(t, string(conditionReasonWalletFundingPending), condition.Reason)
		assert.False(t, degraded)
		assert.Equal(t, 1, funderCalls)
		require.NotNil(t, status)
		assert.Equal(t, "tx-abc", status.FundedTxID)
		assert.False(t, status.Funded)
	})

	t.Run("does not resubmit while a tx is in flight", func(t *testing.T) {
		funderCalls := 0
		reconciler, network, settings, kupoSvc, faucetSvc := setup(t,
			walletConfirmerFunc(func(context.Context, string, string, int64) (bool, error) { return false, nil }),
			walletFunderFunc(func(context.Context, string, string, string, int64) (string, error) {
				funderCalls++
				return "tx-new", nil
			}), "tx-prev")
		condition, status, degraded, err := reconciler.primaryWalletReadyCondition(ctx, network, settings, conditionTrue(conditionReasonFaucetReady), conditionTrue(conditionReasonKupoReady), kupoSvc, faucetSvc)
		require.NoError(t, err)
		assert.Equal(t, string(conditionReasonWalletFundingPending), condition.Reason)
		assert.Equal(t, 0, funderCalls, "must not resubmit while awaiting confirmation")
		require.NotNil(t, status)
		assert.Equal(t, "tx-prev", status.FundedTxID)
		assert.False(t, degraded)
	})

	t.Run("funding error degrades", func(t *testing.T) {
		reconciler, network, settings, kupoSvc, faucetSvc := setup(t,
			walletConfirmerFunc(func(context.Context, string, string, int64) (bool, error) { return false, nil }),
			walletFunderFunc(func(context.Context, string, string, string, int64) (string, error) {
				return "", errors.New("faucet rejected")
			}), "")
		condition, _, degraded, err := reconciler.primaryWalletReadyCondition(ctx, network, settings, conditionTrue(conditionReasonFaucetReady), conditionTrue(conditionReasonKupoReady), kupoSvc, faucetSvc)
		require.NoError(t, err)
		assert.Equal(t, string(conditionReasonWalletFundingFailed), condition.Reason)
		assert.True(t, degraded)
	})

	t.Run("confirmation error degrades", func(t *testing.T) {
		reconciler, network, settings, kupoSvc, faucetSvc := setup(t,
			walletConfirmerFunc(func(context.Context, string, string, int64) (bool, error) { return false, errors.New("kupo down") }),
			walletFunderFunc(func(context.Context, string, string, string, int64) (string, error) { return "", nil }), "")
		condition, _, degraded, err := reconciler.primaryWalletReadyCondition(ctx, network, settings, conditionTrue(conditionReasonFaucetReady), conditionTrue(conditionReasonKupoReady), kupoSvc, faucetSvc)
		require.NoError(t, err)
		assert.Equal(t, string(conditionReasonWalletFundingFailed), condition.Reason)
		assert.True(t, degraded)
	})
}

func serviceWithPort(namespace, name string, port int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: port}}},
	}
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
