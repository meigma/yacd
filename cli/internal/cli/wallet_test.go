package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/mocks"
	walletstore "github.com/meigma/yacd/cli/internal/wallet"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	"github.com/meigma/yacd/internal/cardano/tx"
	domainwallet "github.com/meigma/yacd/internal/cardano/wallet"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// testNetwork is the network name and namespace the wallet verb tests operate
// against, matching readyNetwork.
const testNetwork = "devnet"

// walletFixture is a generated wallet with both its key material and the Secret
// the controller/CLI would persist for it.
type walletFixture struct {
	name     string
	material domainwallet.Material
	secret   corev1.Secret
}

// newWalletFixture builds a wallet for the test network from a deterministic
// seed so the decoded signing/verification key hex is stable across runs, and
// renders the backing Secret with the network instance label, name/source
// labels, and data keys the store reads.
func newWalletFixture(t *testing.T, name string, source string, seed byte) walletFixture {
	t.Helper()

	rawSeed := bytes.Repeat([]byte{seed}, 32)
	material, err := domainwallet.FromSeed(rawSeed)
	require.NoError(t, err, "generate fixture wallet material")

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      walletstore.SecretName(testNetwork, name),
			Namespace: testNetwork,
			Labels: map[string]string{
				primarypod.LabelCardanoNetwork: testNetwork,
				walletstore.NameLabel:          name,
				walletstore.SourceLabel:        source,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			walletstore.SigningKeyKey:      material.SigningKeyEnvelope,
			walletstore.VerificationKeyKey: material.VerificationKeyEnvelope,
			walletstore.AddressKey:         []byte(material.Address),
		},
	}

	return walletFixture{name: name, material: material, secret: secret}
}

// walletNetworkSelector is the label selector the store scopes its Secret reads
// by for the test network.
func walletNetworkSelector() map[string]string {
	return map[string]string{primarypod.LabelCardanoNetwork: testNetwork}
}

// pubKeyHex decodes the fixture's verification key envelope to the raw pubkey
// hex selector form.
func (f walletFixture) pubKeyHex(t *testing.T) string {
	t.Helper()
	hexValue, err := domainwallet.DecodePaymentKeyEnvelope(f.material.VerificationKeyEnvelope)
	require.NoError(t, err)

	return hexValue
}

func TestWalletListRendersManagedWalletsExcludingFaucet(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)
	alice := newWalletFixture(t, "alice", walletstore.SourceManagedByCLI, 0x02)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{faucet.secret, alice.secret}, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "list", "devnet"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	out := stdout.String()
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, alice.material.Address)
	assert.NotContains(t, out, "faucet", "the reserved faucet wallet must not appear in the managed listing")
}

func TestWalletAddGeneratesWalletAndCreatesSecret(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// No --name: chooseName lists existing wallets to avoid a collision.
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).Return(nil, nil)

	var created *corev1.Secret
	client.EXPECT().CreateSecret(mock.Anything, mock.Anything).
		Run(func(_ context.Context, secret *corev1.Secret) { created = secret }).
		Return(nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "add", "devnet"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, created)
	assert.Equal(t, walletstore.SourceManagedByCLI, created.Labels[walletstore.SourceLabel])
	assert.NotEmpty(t, created.Labels[walletstore.NameLabel])
	assert.Contains(t, created.Data, walletstore.SigningKeyKey)
	assert.Contains(t, created.Data, walletstore.AddressKey)
	require.Len(t, created.OwnerReferences, 1, "created wallet must be owned by the network")
	assert.Equal(t, "CardanoNetwork", created.OwnerReferences[0].Kind)
}

func TestWalletAddWithExplicitNameRejectsFaucet(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "add", "devnet", "--name", "faucet"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "faucet wallet is reserved")
	client.AssertNotCalled(t, "CreateSecret", mock.Anything, mock.Anything)
}

func TestWalletAddRejectsWhenCeilingReached(t *testing.T) {
	t.Parallel()

	secrets := make([]corev1.Secret, 0, walletstore.MaxManagedWallets+1)
	for i := range walletstore.MaxManagedWallets {
		fixture := newWalletFixture(t, "wallet"+string(rune('a'+i)), walletstore.SourceManagedByCLI, byte(0x10+i))
		secrets = append(secrets, fixture.secret)
	}
	// The faucet is present in the listing but must not count toward the managed
	// ceiling: with exactly MaxManagedWallets managed wallets plus the faucet, the
	// add must still be rejected, not silently accept one more.
	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)
	secrets = append(secrets, faucet.secret)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).Return(secrets, nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "add", "devnet"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capacity reached")
	client.AssertNotCalled(t, "CreateSecret", mock.Anything, mock.Anything)
}

func TestWalletTopUpFundsResolvedDestinationFromFaucet(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)
	bob := newWalletFixture(t, "bob", walletstore.SourceManagedByCLI, 0x02)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// Resolve "bob" by name lists wallets; Source(faucet) reads the faucet Secret.
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{faucet.secret, bob.secret}, nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", faucet.secret.Name).Return(&faucet.secret, nil)

	var gotRequest tx.Request
	var gotOgmios, gotKupo string
	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).
		Run(func(_ context.Context, request tx.Request) { gotRequest = request }).
		Return(tx.Result{TxID: "feedface"}, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Err:               &bytes.Buffer{},
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
		TxSubmitterFactory: func(ogmiosURL string, kupoURL string) tx.Submitter {
			gotOgmios, gotKupo = ogmiosURL, kupoURL
			return submitter
		},
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", "bob", "2000000"})

	require.NoError(t, root.ExecuteContext(context.Background()))

	assert.Equal(t, "ws://127.0.0.1:40001", gotOgmios)
	assert.Equal(t, "http://127.0.0.1:40002", gotKupo)
	assert.Equal(t, walletstore.FaucetWalletName, gotRequest.SourceName)
	assert.Equal(t, faucet.material.Address, gotRequest.SourceAddress)
	assert.Equal(t, bob.material.Address, gotRequest.DestinationAddress)
	assert.Equal(t, int64(2000000), gotRequest.Lovelace)
	assert.Equal(t, faucet.pubKeyHex(t), gotRequest.VerificationKeyHex)
	assert.Contains(t, stdout.String(), "feedface")
}

func TestWalletTopUpFundsFromNamedManagedWallet(t *testing.T) {
	t.Parallel()

	source := newWalletFixture(t, "treasury", walletstore.SourceManagedByCLI, 0x0a)
	bob := newWalletFixture(t, "bob", walletstore.SourceManagedByCLI, 0x02)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// Resolving "bob" by name and resolving the --from source both read the
	// network's wallet Secrets.
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{source.secret, bob.secret}, nil)
	// The source's signing material is read from its own Secret, not the faucet's.
	client.EXPECT().GetSecret(mock.Anything, "devnet", source.secret.Name).Return(&source.secret, nil)

	var gotRequest tx.Request
	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).
		Run(func(_ context.Context, request tx.Request) { gotRequest = request }).
		Return(tx.Result{TxID: "fromtx"}, nil)

	root := NewRootCommand(Options{
		Out:                &bytes.Buffer{},
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return submitter },
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", "bob", "2000000", "--from", "treasury"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "treasury", gotRequest.SourceName, "funds must be drawn from the --from wallet")
	assert.Equal(t, source.material.Address, gotRequest.SourceAddress)
	assert.Equal(t, bob.material.Address, gotRequest.DestinationAddress)
}

func TestWalletTopUpFromUnknownWalletFails(t *testing.T) {
	t.Parallel()

	bob := newWalletFixture(t, "bob", walletstore.SourceManagedByCLI, 0x02)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// "bob" resolves by name; the --from source is absent from the same listing,
	// so resolving the source must fail before any forwarding or submit.
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{bob.secret}, nil)

	root := NewRootCommand(Options{
		Out:               &bytes.Buffer{},
		Err:               &bytes.Buffer{},
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
		// A submitter that would fail loudly if funding proceeded.
		TxSubmitterFactory: func(string, string) tx.Submitter { return mocks.NewSubmitter(t) },
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", "bob", "2000000", "--from", "ghost"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `source wallet "ghost" not found`)
}

func TestWalletTopUpFromSelfFails(t *testing.T) {
	t.Parallel()

	source := newWalletFixture(t, "treasury", walletstore.SourceManagedByCLI, 0x0a)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// The destination is the source's own raw address, so funding from "treasury"
	// would be a self-transfer. Resolving the source reads the network wallets;
	// the same-address guard fails before any forwarding or submit.
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{source.secret}, nil)

	root := NewRootCommand(Options{
		Out:                &bytes.Buffer{},
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return mocks.NewSubmitter(t) },
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", source.material.Address, "2000000", "--from", "treasury"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same address")
}

func TestWalletTopUpFailsWhenNetworkNotReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(network *yacdv1alpha1.CardanoNetwork)
		wantErr string
	}{
		{
			name: "ready condition is false",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				setReadyStatus(network, metav1.ConditionFalse)
			},
			wantErr: "not ready",
		},
		{
			name: "status observed an older generation",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Generation = 2 // status still reports generation 1 -> stale
			},
			wantErr: "stale",
		},
		{
			name: "network is degraded",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Status.Conditions = append(network.Status.Conditions, metav1.Condition{
					Type:               "Degraded",
					Status:             metav1.ConditionTrue,
					Reason:             "UnsupportedSpec",
					Message:            "bad",
					ObservedGeneration: 1,
				})
			},
			wantErr: "degraded",
		},
		{
			name: "ready condition is missing",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Status.Conditions = nil
			},
			wantErr: "not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := newWalletFixture(t, "dst", walletstore.SourceManagedByCLI, 0x09)
			network := readyNetwork("devnet")
			tt.mutate(network)

			client := newKubeMock(t)
			client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
			// No PrimaryPodName/Forward/GetSecret expectations: the readiness gate
			// must fail before any cluster work or funding.

			root := NewRootCommand(Options{
				Out:               &bytes.Buffer{},
				Err:               &bytes.Buffer{},
				Viper:             viper.New(),
				KubeClientFactory: kubeClientFactory(client),
				// A submitter that would fail the mock's cleanup if funding proceeded.
				TxSubmitterFactory: func(string, string) tx.Submitter { return mocks.NewSubmitter(t) },
			})
			// A raw destination address keeps the test focused on the readiness gate
			// without an intervening selector lookup.
			root.SetArgs([]string{"wallet", "topup", "devnet", target.material.Address, "1000000"})

			err := root.ExecuteContext(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestWalletTopUpResolvesRawAddressWithoutClusterLookup(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)
	target := newWalletFixture(t, "ignored", walletstore.SourceManagedByCLI, 0x07)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// A raw bech32 address resolves with no ListSecrets; only the faucet source
	// Secret is read.
	client.EXPECT().GetSecret(mock.Anything, "devnet", faucet.secret.Name).Return(&faucet.secret, nil)

	var gotRequest tx.Request
	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).
		Run(func(_ context.Context, request tx.Request) { gotRequest = request }).
		Return(tx.Result{TxID: "abc"}, nil)

	root := NewRootCommand(Options{
		Out:                &bytes.Buffer{},
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return submitter },
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", target.material.Address, "1000000"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, target.material.Address, gotRequest.DestinationAddress)
}

func TestWalletTopUpResolvesByPubKey(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)
	carol := newWalletFixture(t, "carol", walletstore.SourceManagedByCLI, 0x03)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{faucet.secret, carol.secret}, nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", faucet.secret.Name).Return(&faucet.secret, nil)

	var gotRequest tx.Request
	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).
		Run(func(_ context.Context, request tx.Request) { gotRequest = request }).
		Return(tx.Result{TxID: "abc"}, nil)

	root := NewRootCommand(Options{
		Out:                &bytes.Buffer{},
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return submitter },
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", carol.pubKeyHex(t), "1000000"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, carol.material.Address, gotRequest.DestinationAddress)
}

func TestWalletTopUpFailsWhenNoFaucetWallet(t *testing.T) {
	t.Parallel()

	target := newWalletFixture(t, "dst", walletstore.SourceManagedByCLI, 0x09)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	// The destination is a raw address (no list); the faucet Secret is absent.
	client.EXPECT().GetSecret(mock.Anything, "devnet", walletstore.SecretName("devnet", walletstore.FaucetWalletName)).
		Return(nil, walletNotFound())

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
		// A submitter factory that would fail loudly if funding proceeded.
		TxSubmitterFactory: func(string, string) tx.Submitter { return mocks.NewSubmitter(t) },
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", target.material.Address, "1000000"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not funding-ready")
}

func TestWalletTopUpAwaitsConfirmation(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)
	dave := newWalletFixture(t, "dave", walletstore.SourceManagedByCLI, 0x04)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().ListSecrets(mock.Anything, "devnet", walletNetworkSelector()).
		Return([]corev1.Secret{faucet.secret, dave.secret}, nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", faucet.secret.Name).Return(&faucet.secret, nil)

	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).Return(tx.Result{TxID: "txconfirm"}, nil)

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, dave.material.Address).Return([]string{"txconfirm"}, nil)
	var gotKupoURL string

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:                &stdout,
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return submitter },
		UTxOConfirmerFactory: func(kupoURL string) UTxOConfirmer {
			gotKupoURL = kupoURL
			return confirmer
		},
	})
	root.SetArgs([]string{"wallet", "topup", "devnet", "dave", "2000000", "--await"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "http://127.0.0.1:40002", gotKupoURL, "--await must poll the forwarded loopback Kupo")
	assert.Contains(t, stdout.String(), "Confirmed on-chain.")
}

func TestWalletAddWithTopupFundsCreatedWallet(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)

	var createdAddress string
	client.EXPECT().CreateSecret(mock.Anything, mock.Anything).
		Run(func(_ context.Context, secret *corev1.Secret) {
			createdAddress = string(secret.Data[walletstore.AddressKey])
		}).
		Return(nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", faucet.secret.Name).Return(&faucet.secret, nil)

	var gotRequest tx.Request
	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).
		Run(func(_ context.Context, request tx.Request) { gotRequest = request }).
		Return(tx.Result{TxID: "fundtx"}, nil)

	root := NewRootCommand(Options{
		Out:                &bytes.Buffer{},
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return submitter },
	})
	root.SetArgs([]string{"wallet", "add", "devnet", "--name", "newbie", "--topup", "5000000"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, createdAddress, gotRequest.DestinationAddress, "the new wallet's address must be funded")
	assert.Equal(t, int64(5000000), gotRequest.Lovelace)
}

func TestWalletAddWithTopupPersistsWalletWhenFundingFails(t *testing.T) {
	t.Parallel()

	faucet := newWalletFixture(t, walletstore.FaucetWalletName, walletstore.SourceGenesisFunded, 0x01)

	client := walletForwardMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)

	// The Secret is created first; funding then fails. The wallet must survive the
	// funding failure, so no DeleteSecret may be issued.
	client.EXPECT().CreateSecret(mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", faucet.secret.Name).Return(&faucet.secret, nil)

	submitter := mocks.NewSubmitter(t)
	submitter.EXPECT().Submit(mock.Anything, mock.Anything).
		Return(tx.Result{}, fmt.Errorf("chain rejected the funding transaction"))

	root := NewRootCommand(Options{
		Out:                &bytes.Buffer{},
		Err:                &bytes.Buffer{},
		Viper:              viper.New(),
		KubeClientFactory:  kubeClientFactory(client),
		TxSubmitterFactory: func(string, string) tx.Submitter { return submitter },
	})
	root.SetArgs([]string{"wallet", "add", "devnet", "--name", "newbie", "--topup", "5000000"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err, "the funding failure must propagate to the caller")
	assert.Contains(t, err.Error(), "chain rejected the funding transaction")
	// The mock's cleanup asserts DeleteSecret was never expected and never called,
	// so the created wallet Secret is left in place.
	client.AssertNotCalled(t, "DeleteSecret", mock.Anything, mock.Anything, mock.Anything)
}

func TestWalletRemoveRejectsFaucet(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "remove", "devnet", "faucet"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "faucet wallet is reserved")
	client.AssertNotCalled(t, "DeleteSecret", mock.Anything, mock.Anything, mock.Anything)
}

func TestWalletRemoveDeletesSecret(t *testing.T) {
	t.Parallel()

	alice := newWalletFixture(t, "alice", walletstore.SourceManagedByCLI, 0x05)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", alice.secret.Name).Return(&alice.secret, nil)
	client.EXPECT().DeleteSecret(mock.Anything, "devnet", walletstore.SecretName("devnet", "alice")).Return(nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "remove", "devnet", "alice"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stdout.String(), "Removed wallet")
}

func TestWalletRemoveReportsMissingWallet(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecret(mock.Anything, "devnet", walletstore.SecretName("devnet", "ghost")).
		Return(nil, kube.ErrNotFound)

	root := NewRootCommand(Options{
		Out:               &bytes.Buffer{},
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "remove", "devnet", "ghost"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `wallet "ghost" not found`)
	client.AssertNotCalled(t, "DeleteSecret", mock.Anything, mock.Anything, mock.Anything)
}

func TestWalletExportWritesKeyFilesWithRestrictivePerms(t *testing.T) {
	t.Parallel()

	alice := newWalletFixture(t, "alice", walletstore.SourceManagedByCLI, 0x05)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", alice.secret.Name).Return(&alice.secret, nil)

	// Point --out at a not-yet-existing subdirectory so export creates it and we
	// can assert the 0700 mode it sets, rather than inheriting t.TempDir's mode.
	dir := filepath.Join(t.TempDir(), "wallets", "alice")
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "export", "devnet", "alice", "--out", dir})

	require.NoError(t, root.ExecuteContext(context.Background()))

	for _, suffix := range []string{".skey", ".vkey", ".addr"} {
		path := filepath.Join(dir, "alice"+suffix)
		info, err := os.Stat(path)
		require.NoError(t, err, "expected %s to be written", path)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "%s must be 0600", path)
	}
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "export directory must be 0700")

	addr, err := os.ReadFile(filepath.Join(dir, "alice.addr"))
	require.NoError(t, err)
	assert.Equal(t, alice.material.Address+"\n", string(addr))
	// Key material must not leak to stdout.
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "alice.skey")
}

func TestWalletExportRefusesToOverwriteWithoutForce(t *testing.T) {
	t.Parallel()

	alice := newWalletFixture(t, "alice", walletstore.SourceManagedByCLI, 0x05)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().GetSecret(mock.Anything, "devnet", alice.secret.Name).Return(&alice.secret, nil)

	dir := t.TempDir()
	// Pre-create the .skey so the export collides on the first file it writes.
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alice.skey"), []byte("existing"), 0o600))

	root := NewRootCommand(Options{
		Out:               &bytes.Buffer{},
		Err:               &bytes.Buffer{},
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"wallet", "export", "devnet", "alice", "--out", dir})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestWalletExportValidatesSecretData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, client *mocks.Client)
		wantErr string
	}{
		{
			name: "secret not found",
			setup: func(t *testing.T, client *mocks.Client) {
				client.EXPECT().GetSecret(mock.Anything, "devnet", walletstore.SecretName("devnet", "alice")).
					Return(nil, walletNotFound())
			},
			wantErr: "not found",
		},
		{
			name: "missing signing key",
			setup: func(t *testing.T, client *mocks.Client) {
				secret := exportSecretWithout(t, walletstore.SigningKeyKey)
				client.EXPECT().GetSecret(mock.Anything, "devnet", secret.Name).Return(&secret, nil)
			},
			wantErr: walletstore.SigningKeyKey,
		},
		{
			name: "missing verification key",
			setup: func(t *testing.T, client *mocks.Client) {
				secret := exportSecretWithout(t, walletstore.VerificationKeyKey)
				client.EXPECT().GetSecret(mock.Anything, "devnet", secret.Name).Return(&secret, nil)
			},
			wantErr: walletstore.VerificationKeyKey,
		},
		{
			name: "missing address",
			setup: func(t *testing.T, client *mocks.Client) {
				secret := exportSecretWithout(t, walletstore.AddressKey)
				client.EXPECT().GetSecret(mock.Anything, "devnet", secret.Name).Return(&secret, nil)
			},
			wantErr: walletstore.AddressKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newKubeMock(t)
			client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
			tt.setup(t, client)

			// Point --out at a fresh dir so a leaked write would be observable; the
			// export must fail before writeWalletFiles touches disk.
			dir := filepath.Join(t.TempDir(), "out")
			root := NewRootCommand(Options{
				Out:               &bytes.Buffer{},
				Err:               &bytes.Buffer{},
				Viper:             viper.New(),
				KubeClientFactory: kubeClientFactory(client),
			})
			root.SetArgs([]string{"wallet", "export", "devnet", "alice", "--out", dir})

			err := root.ExecuteContext(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			_, statErr := os.Stat(dir)
			assert.True(t, os.IsNotExist(statErr), "no files may be written when the Secret is invalid")
		})
	}
}

// exportSecretWithout builds an "alice" wallet Secret with one required data key
// removed, so the export verb's data validation can be exercised per key.
func exportSecretWithout(t *testing.T, missingKey string) corev1.Secret {
	t.Helper()

	fixture := newWalletFixture(t, "alice", walletstore.SourceManagedByCLI, 0x05)
	delete(fixture.secret.Data, missingKey)

	return fixture.secret
}

// walletForwardMock wires a mock kube.Client with a ForwardSession for the
// funding self-forward: a ready network's primary Pod, a forward mapping the
// published Ogmios/Kupo/faucet container ports to fixed loopback ports, and a
// Close on teardown. The funding path reads loopback URLs but never supervises
// the session, so only LocalPort and Close are exercised.
func walletForwardMock(t *testing.T) *mocks.Client {
	t.Helper()

	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().LocalPort(int32(8080)).Return(40003, true).Maybe()
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)

	return client
}

// walletNotFound returns the port's not-found sentinel so a mock can simulate a
// missing Secret the store branches on via kube.IsNotFound.
func walletNotFound() error {
	return kube.ErrNotFound
}

// setReadyStatus flips the network's Ready condition to status, leaving the rest
// of the published readyNetwork shape intact so funding tests can assert on the
// readiness gate alone.
func setReadyStatus(network *yacdv1alpha1.CardanoNetwork, status metav1.ConditionStatus) {
	for i := range network.Status.Conditions {
		if network.Status.Conditions[i].Type == "Ready" {
			network.Status.Conditions[i].Status = status
		}
	}
}
