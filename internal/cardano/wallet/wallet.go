package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	apolloAddress "github.com/Salvionied/apollo/serialization/Address"
	apolloKey "github.com/Salvionied/apollo/serialization/Key"
	"github.com/fxamacker/cbor/v2"
)

const (
	// seedLength is the ed25519 seed / raw key size in bytes.
	seedLength = 32

	signingKeyType        = "PaymentSigningKeyShelley_ed25519"
	signingKeyDescription = "Payment Signing Key"

	verificationKeyType        = "PaymentVerificationKeyShelley_ed25519"
	verificationKeyDescription = "Payment Verification Key"

	// addressHRP is the bech32 human-readable prefix for testnet addresses.
	addressHRP = "addr_test"

	// testnetEnterpriseHeader is the Shelley address header byte for an
	// enterprise (payment-key-only, no staking) address on a testnet:
	// type nibble 0b0110 (KEY_NONE) followed by network nibble 0 (TESTNET).
	testnetEnterpriseHeader = 0b01100000
)

// Material is a generated developer wallet: the cardano-cli text-envelope key
// files plus the derived enterprise testnet address. The envelopes are the
// exact bytes to persist as Secret data and to hand to a developer for
// cardano-cli use.
type Material struct {
	// Address is the bech32 enterprise testnet address (addr_test1...).
	Address string
	// SigningKeyEnvelope is the PaymentSigningKeyShelley_ed25519 text envelope.
	SigningKeyEnvelope []byte
	// VerificationKeyEnvelope is the PaymentVerificationKeyShelley_ed25519
	// text envelope.
	VerificationKeyEnvelope []byte
}

// New generates a fresh developer wallet using crypto/rand.
func New() (Material, error) {
	seed := make([]byte, seedLength)
	if _, err := rand.Read(seed); err != nil {
		return Material{}, fmt.Errorf("read wallet key seed: %w", err)
	}

	return FromSeed(seed)
}

// FromSeed builds a developer wallet from a fixed 32-byte ed25519 seed. It is
// deterministic, which makes it the basis for golden tests; production code
// uses New.
func FromSeed(seed []byte) (Material, error) {
	if len(seed) != seedLength {
		return Material{}, fmt.Errorf("wallet seed must be %d bytes, got %d", seedLength, len(seed))
	}

	private := ed25519.NewKeyFromSeed(seed)
	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		return Material{}, fmt.Errorf("derive wallet public key")
	}

	address, err := DeriveTestnetAddress(public)
	if err != nil {
		return Material{}, err
	}

	signingEnvelope, err := keyEnvelope(signingKeyType, signingKeyDescription, seed)
	if err != nil {
		return Material{}, fmt.Errorf("encode signing key: %w", err)
	}
	verificationEnvelope, err := keyEnvelope(verificationKeyType, verificationKeyDescription, public)
	if err != nil {
		return Material{}, fmt.Errorf("encode verification key: %w", err)
	}

	return Material{
		Address:                 address,
		SigningKeyEnvelope:      signingEnvelope,
		VerificationKeyEnvelope: verificationEnvelope,
	}, nil
}

// DeriveTestnetAddress returns the enterprise testnet address (addr_test...)
// for a raw 32-byte ed25519 payment verification key. It is the shared
// derivation used by both the operator and the faucet so an address is never
// computed two different ways.
func DeriveTestnetAddress(verificationKey []byte) (string, error) {
	if len(verificationKey) != seedLength {
		return "", fmt.Errorf("verification key must be %d bytes, got %d", seedLength, len(verificationKey))
	}

	paymentHash, err := apolloKey.VerificationKey{Payload: verificationKey}.Hash()
	if err != nil {
		return "", fmt.Errorf("hash verification key: %w", err)
	}

	address := apolloAddress.Address{
		PaymentPart: paymentHash[:],
		Network:     apolloAddress.TESTNET,
		AddressType: apolloAddress.KEY_NONE,
		HeaderByte:  testnetEnterpriseHeader,
		Hrp:         addressHRP,
	}

	return address.String(), nil
}

// textEnvelope is the cardano-cli key-file format: a JSON object with a type,
// a human description, and the CBOR-encoded raw key as hex.
type textEnvelope struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	CBORHex     string `json:"cborHex"`
}

// keyEnvelope encodes a raw 32-byte key as a cardano-cli text envelope. The
// cborHex wraps the raw bytes in a CBOR byte string (the on-disk convention
// cardano-cli reads back), and the JSON is pretty-printed with a trailing
// newline to match cardano-cli output.
func keyEnvelope(keyType string, description string, raw []byte) ([]byte, error) {
	cborBytes, err := cbor.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("cbor-encode key: %w", err)
	}

	encoded, err := json.MarshalIndent(textEnvelope{
		Type:        keyType,
		Description: description,
		CBORHex:     hex.EncodeToString(cborBytes),
	}, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("marshal key envelope: %w", err)
	}

	return append(encoded, '\n'), nil
}
