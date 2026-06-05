package wallet

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	domainwallet "github.com/meigma/yacd/internal/cardano/wallet"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// SigningKeyKey is the Secret data key for the wallet's payment signing key
	// text envelope. It mirrors the controller's primary wallet Secret shape.
	SigningKeyKey = "payment.skey"

	// VerificationKeyKey is the Secret data key for the wallet's payment
	// verification key text envelope.
	VerificationKeyKey = "payment.vkey"

	// AddressKey is the Secret data key for the wallet's bech32 testnet address.
	AddressKey = "address"

	// NameLabel marks an owned wallet Secret with its well-known wallet name so
	// the CLI can select a specific wallet without parsing the Secret name. It
	// matches the controller's faucet wallet label key.
	NameLabel = "yacd.meigma.io/wallet-name"

	// SourceLabel records how a wallet Secret is funded. CLI-created wallets
	// carry SourceManagedByCLI; the genesis-funded faucet wallet carries the
	// controller's genesis-funded value.
	SourceLabel = "yacd.meigma.io/wallet-source"

	// SourceManagedByCLI is the SourceLabel value for a wallet the CLI created.
	SourceManagedByCLI = "managed-by-cli"

	// SourceGenesisFunded is the SourceLabel value the controller stamps on the
	// genesis-funded faucet wallet.
	SourceGenesisFunded = "genesis-funded"

	// FaucetWalletName is the reserved wallet name for the genesis-funded faucet
	// wallet. It is excluded from managed listings and may not be created or
	// removed through the CLI.
	FaucetWalletName = "faucet"

	// walletSecretSuffix is the per-name suffix the wallet Secret name carries
	// after the network name, mirroring the controller's "<net>-wallet-<name>"
	// convention. The faucet wallet uses "wallet-faucet".
	walletSecretSuffix = "wallet"
)

// ErrFaucetReserved is returned when a caller attempts to create or remove the
// reserved faucet wallet through the CLI.
var ErrFaucetReserved = errors.New("the faucet wallet is reserved and cannot be created or removed through the CLI")

// ManagedWallet is a CLI-visible view of a wallet Secret. It never carries
// signing material: the signing key stays in the Secret and is read only on the
// explicit export path.
type ManagedWallet struct {
	// Name is the well-known wallet name from the NameLabel.
	Name string

	// Address is the bech32 enterprise testnet address the wallet controls.
	Address string

	// Source is the SourceLabel value describing how the wallet is funded.
	Source string

	// SecretName is the name of the backing Secret in the network namespace.
	SecretName string
}

// Store is the managed-wallet repository over the CLI's Kubernetes Secret port.
// It owns the wallet Secret naming, labelling, and ownership conventions so the
// command layer works in terms of wallet names and addresses rather than Secret
// shapes.
type Store struct {
	client    kube.Client
	namespace string
	network   string
}

// NewStore returns a wallet Store bound to a network's namespace. network is the
// CardanoNetwork name, used to derive Secret names and the instance label.
func NewStore(client kube.Client, namespace string, network string) *Store {
	return &Store{
		client:    client,
		namespace: namespace,
		network:   network,
	}
}

// List returns the managed wallets for the network, excluding the reserved
// faucet wallet, sorted by name. It selects on the network instance label so it
// never returns wallets from another network sharing the namespace.
func (s *Store) List(ctx context.Context) ([]ManagedWallet, error) {
	secrets, err := s.walletSecrets(ctx)
	if err != nil {
		return nil, err
	}

	wallets := make([]ManagedWallet, 0, len(secrets))
	for i := range secrets {
		wallet := walletFromSecret(&secrets[i])
		if wallet.Name == FaucetWalletName {
			continue
		}
		wallets = append(wallets, wallet)
	}
	sort.Slice(wallets, func(i, j int) bool {
		return wallets[i].Name < wallets[j].Name
	})

	return wallets, nil
}

// Faucet returns the genesis-funded faucet wallet, which is the default funding
// source. A missing faucet wallet is reported as a typed error so callers can
// explain that the network is not funding-ready rather than surfacing a raw
// not-found.
func (s *Store) Faucet(ctx context.Context) (ManagedWallet, error) {
	secret, err := s.client.GetSecret(ctx, s.namespace, s.faucetSecretName())
	if err != nil {
		if kube.IsNotFound(err) {
			return ManagedWallet{}, fmt.Errorf("network %q has no faucet wallet yet; the network is not funding-ready", s.network)
		}
		return ManagedWallet{}, err
	}

	return walletFromSecret(secret), nil
}

// Source resolves the wallet that funds a transfer and returns its full view,
// including the backing Secret name so the caller can read its signing key.
//
// An empty name selects the genesis-funded faucet wallet, the default funding
// source. A non-empty name selects a managed wallet by its name label; the
// reserved faucet name is accepted so a caller may name it explicitly. A name
// matching no wallet is reported as not found.
func (s *Store) Source(ctx context.Context, name string) (ManagedWallet, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == FaucetWalletName {
		return s.Faucet(ctx)
	}

	secrets, err := s.walletSecrets(ctx)
	if err != nil {
		return ManagedWallet{}, err
	}
	for i := range secrets {
		wallet := walletFromSecret(&secrets[i])
		if wallet.Name == name {
			return wallet, nil
		}
	}

	return ManagedWallet{}, fmt.Errorf("source wallet %q not found", name)
}

// Resolve maps a wallet selector to a fundable address. The selector is one of:
// a managed wallet name (resolved through the label set, including the faucet
// wallet), a 32-byte ed25519 public key as hex (matched against managed wallet
// verification keys), or a raw bech32 testnet address (funded directly).
//
// A raw address resolves without any cluster lookup, so an address that does not
// belong to a managed wallet is still fundable. A name or pubkey that matches no
// managed wallet is reported as not found.
func (s *Store) Resolve(ctx context.Context, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("WALLET selector is required")
	}

	if strings.HasPrefix(selector, addressPrefix) {
		if err := validateTestnetAddress(selector); err != nil {
			return "", fmt.Errorf("invalid wallet address %q: %w", selector, err)
		}
		return selector, nil
	}

	secrets, err := s.walletSecrets(ctx)
	if err != nil {
		return "", err
	}

	if pubKeyHex, ok := normalizePubKeyHex(selector); ok {
		return resolveByPubKey(secrets, pubKeyHex)
	}

	return resolveByName(secrets, selector)
}

// Create persists a new managed wallet Secret from freshly generated material
// and a name, owned by the network. It rejects the reserved faucet name and
// surfaces an AlreadyExists collision through the Secret port. The created
// ManagedWallet view is returned for rendering.
func (s *Store) Create(ctx context.Context, name string, material domainwallet.Material, owner *yacdv1alpha1.CardanoNetwork) (ManagedWallet, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == FaucetWalletName {
		return ManagedWallet{}, ErrFaucetReserved
	}
	if name == "" {
		return ManagedWallet{}, fmt.Errorf("wallet name is required")
	}
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return ManagedWallet{}, fmt.Errorf("invalid wallet name %q: must be a lowercase DNS-1123 label (letters, digits, and '-')", name)
	}
	if owner == nil {
		return ManagedWallet{}, fmt.Errorf("owning CardanoNetwork is required")
	}

	secret := s.buildSecret(name, material, owner)
	if err := s.client.CreateSecret(ctx, secret); err != nil {
		return ManagedWallet{}, err
	}

	return walletFromSecret(secret), nil
}

// Delete removes a managed wallet Secret by name. It rejects the reserved faucet
// name and is idempotent for an already-absent wallet through the Secret port.
func (s *Store) Delete(ctx context.Context, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == FaucetWalletName {
		return ErrFaucetReserved
	}
	if name == "" {
		return fmt.Errorf("wallet name is required")
	}

	return s.client.DeleteSecret(ctx, s.namespace, s.secretName(name))
}

// buildSecret renders the owned wallet Secret for a name and material. The label
// set mirrors the controller's primary workload labels plus the wallet name and
// CLI source markers, and the owner reference ties the Secret's lifecycle to the
// CardanoNetwork so deleting the network garbage-collects the wallet.
func (s *Store) buildSecret(name string, material domainwallet.Material, owner *yacdv1alpha1.CardanoNetwork) *corev1.Secret {
	labels := primarypod.SelectorLabels(owner)
	labels[primarypod.LabelAppManagedBy] = "yacd"
	labels[NameLabel] = name
	labels[SourceLabel] = SourceManagedByCLI

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.secretName(name),
			Namespace: s.namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				ownerReference(owner),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SigningKeyKey:      material.SigningKeyEnvelope,
			VerificationKeyKey: material.VerificationKeyEnvelope,
			AddressKey:         []byte(material.Address),
		},
	}
}

// walletSecrets lists the network's wallet Secrets: those carrying the network
// instance label and a wallet-name marker. It includes the genesis-funded faucet
// wallet so name and pubkey resolution can target it; List drops the faucet for
// the managed listing. The name marker is a presence concern, so it is filtered
// in-process rather than through an equality selector.
func (s *Store) walletSecrets(ctx context.Context) ([]corev1.Secret, error) {
	secrets, err := s.client.ListSecrets(ctx, s.namespace, s.networkSelector())
	if err != nil {
		return nil, err
	}

	wallets := secrets[:0]
	for i := range secrets {
		if _, ok := secrets[i].Labels[NameLabel]; ok {
			wallets = append(wallets, secrets[i])
		}
	}

	return wallets, nil
}

// networkSelector scopes Secret reads to this network's owned resources by the
// instance label, so a namespace shared by several networks never leaks another
// network's wallets.
func (s *Store) networkSelector() map[string]string {
	return map[string]string{
		primarypod.LabelCardanoNetwork: ctrlnames.LabelValue(s.network),
	}
}

// secretName renders the Secret name for a managed wallet, mirroring the
// controller's "<net>-wallet-<name>" convention.
func (s *Store) secretName(name string) string {
	return SecretName(s.network, name)
}

// SecretName renders the backing Secret name for a managed wallet on a network,
// mirroring the controller's "<net>-wallet-<name>" convention. It is exported so
// callers that hold only the network and wallet names (for example the export
// verb) can locate the Secret without constructing a Store.
func SecretName(network string, name string) string {
	return ctrlnames.DNSLabelWithSuffix(network, walletSecretSuffix+"-"+name)
}

// faucetSecretName renders the faucet wallet Secret name, mirroring the
// controller's "<net>-wallet-faucet".
func (s *Store) faucetSecretName() string {
	return ctrlnames.DNSLabelWithSuffix(s.network, walletSecretSuffix+"-"+FaucetWalletName)
}

// walletFromSecret projects a Secret onto the CLI wallet view, reading the name
// and source from labels and the address from data.
func walletFromSecret(secret *corev1.Secret) ManagedWallet {
	return ManagedWallet{
		Name:       secret.Labels[NameLabel],
		Address:    string(secret.Data[AddressKey]),
		Source:     secret.Labels[SourceLabel],
		SecretName: secret.Name,
	}
}

// ownerReference builds a controlling owner reference from the network to a
// wallet Secret. The CLI constructs the reference explicitly (it has no scheme)
// using the well-known CardanoNetwork GVK.
func ownerReference(owner *yacdv1alpha1.CardanoNetwork) metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true

	return metav1.OwnerReference{
		APIVersion:         yacdv1alpha1.SchemeGroupVersion.String(),
		Kind:               "CardanoNetwork",
		Name:               owner.Name,
		UID:                owner.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}
