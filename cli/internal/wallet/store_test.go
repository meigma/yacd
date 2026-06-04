package wallet

import (
	"context"
	"fmt"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	domainwallet "github.com/meigma/yacd/internal/cardano/wallet"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testNamespace = "devnet"
	testNetwork   = "devnet"
	// goldenSeedHex/goldenPubHex/goldenAddress mirror the domain wallet golden so
	// a generated Secret's verification key resolves back to a known pubkey.
	goldenSeedHex = "0101010101010101010101010101010101010101010101010101010101010101"
	goldenPubHex  = "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"
	goldenAddress = "addr_test1vqxk54m7j3q6mrkevcunryrwf4p7e68c93cjk8gzxkhlkpsffv7s0"
)

type storeContext struct {
	client *mocks.Client
	store  *Store
}

func newStoreContext(t *testing.T) *storeContext {
	t.Helper()
	client := mocks.NewClient(t)

	return &storeContext{
		client: client,
		store:  NewStore(client, testNamespace, testNetwork),
	}
}

// walletSecret builds a wallet Secret fixture as the controller/CLI would label
// it, so store reads exercise the real label and data conventions.
func walletSecret(name string, source string, address string, vkeyEnvelope []byte) corev1.Secret {
	instance := ctrlnames.LabelValue(testNetwork)
	data := map[string][]byte{AddressKey: []byte(address)}
	if vkeyEnvelope != nil {
		data[VerificationKeyKey] = vkeyEnvelope
	}

	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctrlnames.DNSLabelWithSuffix(testNetwork, "wallet-"+name),
			Namespace: testNamespace,
			Labels: map[string]string{
				primarypod.LabelCardanoNetwork: instance,
				NameLabel:                      name,
				SourceLabel:                    source,
			},
		},
		Data: data,
	}
}

func mustMaterial(t *testing.T) domainwallet.Material {
	t.Helper()
	material, err := domainwallet.FromSeed(mustSeed(t))
	require.NoError(t, err)

	return material
}

func mustSeed(t *testing.T) []byte {
	t.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = 0x01
	}

	return seed
}

func TestStoreListExcludesFaucetAndSortsByName(t *testing.T) {
	tc := newStoreContext(t)
	secrets := []corev1.Secret{
		walletSecret("swift-fox", SourceManagedByCLI, "addr_test1xx", nil),
		walletSecret(FaucetWalletName, SourceGenesisFunded, "addr_test1faucet", nil),
		walletSecret("amber-owl", SourceManagedByCLI, "addr_test1yy", nil),
		// A non-wallet Secret carrying the network label but no wallet-name marker
		// must be filtered out.
		{ObjectMeta: metav1.ObjectMeta{
			Name:   "devnet-faucet-auth",
			Labels: map[string]string{primarypod.LabelCardanoNetwork: ctrlnames.LabelValue(testNetwork)},
		}},
	}
	tc.client.EXPECT().
		ListSecrets(mock.Anything, testNamespace, mock.Anything).
		Return(secrets, nil)

	wallets, err := tc.store.List(context.Background())

	require.NoError(t, err)
	require.Len(t, wallets, 2)
	assert.Equal(t, "amber-owl", wallets[0].Name, "wallets must be sorted by name")
	assert.Equal(t, "swift-fox", wallets[1].Name)
	assert.Equal(t, SourceManagedByCLI, wallets[0].Source)
}

func TestStoreFaucet(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(tc *storeContext)
		assertFunc func(t *testing.T, wallet ManagedWallet, err error)
	}{
		{
			name: "returns the genesis-funded faucet wallet",
			setupMocks: func(tc *storeContext) {
				secret := walletSecret(FaucetWalletName, SourceGenesisFunded, "addr_test1faucet", nil)
				tc.client.EXPECT().
					GetSecret(mock.Anything, testNamespace, ctrlnames.DNSLabelWithSuffix(testNetwork, "wallet-faucet")).
					Return(&secret, nil)
			},
			assertFunc: func(t *testing.T, wallet ManagedWallet, err error) {
				require.NoError(t, err)
				assert.Equal(t, FaucetWalletName, wallet.Name)
				assert.Equal(t, "addr_test1faucet", wallet.Address)
			},
		},
		{
			name: "reports not funding-ready when the faucet wallet is absent",
			setupMocks: func(tc *storeContext) {
				tc.client.EXPECT().
					GetSecret(mock.Anything, testNamespace, mock.Anything).
					Return(nil, fmt.Errorf("secret %w", kube.ErrNotFound))
			},
			assertFunc: func(t *testing.T, _ ManagedWallet, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not funding-ready")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newStoreContext(t)
			tt.setupMocks(tc)

			wallet, err := tc.store.Faucet(context.Background())

			tt.assertFunc(t, wallet, err)
		})
	}
}

func TestStoreResolve(t *testing.T) {
	material := mustMaterial(t)
	managed := []corev1.Secret{
		walletSecret("swift-fox", SourceManagedByCLI, goldenAddress, material.VerificationKeyEnvelope),
		walletSecret(FaucetWalletName, SourceGenesisFunded, "addr_test1faucet", nil),
	}

	tests := []struct {
		name        string
		selector    string
		expectList  bool
		wantAddress string
		wantErr     string
	}{
		{
			name:        "resolves a managed wallet by name",
			selector:    "swift-fox",
			expectList:  true,
			wantAddress: goldenAddress,
		},
		{
			name:        "resolves the faucet wallet by name",
			selector:    FaucetWalletName,
			expectList:  true,
			wantAddress: "addr_test1faucet",
		},
		{
			name:        "resolves a managed wallet by pubkey hex",
			selector:    goldenPubHex,
			expectList:  true,
			wantAddress: goldenAddress,
		},
		{
			name:        "funds a raw bech32 address without a cluster lookup",
			selector:    goldenAddress,
			expectList:  false,
			wantAddress: goldenAddress,
		},
		{
			name:       "reports an unknown name",
			selector:   "no-such-wallet",
			expectList: true,
			wantErr:    "not found",
		},
		{
			name:       "reports an unknown pubkey",
			selector:   "00000000000000000000000000000000000000000000000000000000000000aa",
			expectList: true,
			wantErr:    "no managed wallet matches pubkey",
		},
		{
			// A 64-char selector that is not valid hex (it contains 'z') is not a
			// pubkey, so it must fall through to a name lookup and report not found
			// rather than being misread as a malformed pubkey.
			name:       "treats a 64-char non-hex selector as a name",
			selector:   "zzzz0000000000000000000000000000000000000000000000000000000000aa",
			expectList: true,
			wantErr:    "not found",
		},
		{
			name:     "rejects an empty selector",
			selector: "  ",
			wantErr:  "required",
		},
		{
			name:     "rejects a malformed address",
			selector: "addr_test1notvalid",
			wantErr:  "invalid wallet address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newStoreContext(t)
			if tt.expectList {
				tc.client.EXPECT().
					ListSecrets(mock.Anything, testNamespace, mock.Anything).
					Return(append([]corev1.Secret(nil), managed...), nil)
			}

			address, err := tc.store.Resolve(context.Background(), tt.selector)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAddress, address)
		})
	}
}

func TestStoreCreate(t *testing.T) {
	owner := &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: testNetwork, UID: "owner-uid"},
	}
	material := mustMaterial(t)

	t.Run("creates an owned, labelled wallet Secret", func(t *testing.T) {
		tc := newStoreContext(t)
		var created *corev1.Secret
		tc.client.EXPECT().
			CreateSecret(mock.Anything, mock.Anything).
			Run(func(_ context.Context, secret *corev1.Secret) { created = secret }).
			Return(nil)

		wallet, err := tc.store.Create(context.Background(), "Swift-Fox", material, owner)

		require.NoError(t, err)
		assert.Equal(t, "swift-fox", wallet.Name, "name must be lowercased")
		require.NotNil(t, created)
		assert.Equal(t, ctrlnames.DNSLabelWithSuffix(testNetwork, "wallet-swift-fox"), created.Name)
		assert.Equal(t, "swift-fox", created.Labels[NameLabel])
		assert.Equal(t, SourceManagedByCLI, created.Labels[SourceLabel])
		assert.Equal(t, "yacd", created.Labels[primarypod.LabelAppManagedBy])
		assert.Equal(t, ctrlnames.LabelValue(testNetwork), created.Labels[primarypod.LabelCardanoNetwork])
		assert.Equal(t, material.SigningKeyEnvelope, created.Data[SigningKeyKey])
		assert.Equal(t, material.VerificationKeyEnvelope, created.Data[VerificationKeyKey])
		assert.Equal(t, []byte(material.Address), created.Data[AddressKey])
		require.Len(t, created.OwnerReferences, 1)
		owns := created.OwnerReferences[0]
		assert.Equal(t, "CardanoNetwork", owns.Kind)
		assert.Equal(t, owner.UID, owns.UID)
		require.NotNil(t, owns.Controller)
		assert.True(t, *owns.Controller)
	})

	t.Run("rejects the reserved faucet name", func(t *testing.T) {
		tc := newStoreContext(t)

		_, err := tc.store.Create(context.Background(), FaucetWalletName, material, owner)

		require.ErrorIs(t, err, ErrFaucetReserved)
	})

	t.Run("rejects an invalid name", func(t *testing.T) {
		tc := newStoreContext(t)

		_, err := tc.store.Create(context.Background(), "Bad_Name!", material, owner)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid wallet name")
	})
}

func TestStoreDelete(t *testing.T) {
	t.Run("deletes a managed wallet by name", func(t *testing.T) {
		tc := newStoreContext(t)
		tc.client.EXPECT().
			DeleteSecret(mock.Anything, testNamespace, ctrlnames.DNSLabelWithSuffix(testNetwork, "wallet-swift-fox")).
			Return(nil)

		require.NoError(t, tc.store.Delete(context.Background(), "swift-fox"))
	})

	t.Run("rejects deleting the reserved faucet wallet", func(t *testing.T) {
		tc := newStoreContext(t)

		require.ErrorIs(t, tc.store.Delete(context.Background(), FaucetWalletName), ErrFaucetReserved)
	})
}
