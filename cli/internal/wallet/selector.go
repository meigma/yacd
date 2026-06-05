package wallet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	apolloBech32 "github.com/Salvionied/apollo/crypto/bech32"
	apolloAddress "github.com/Salvionied/apollo/serialization/Address"
	domainwallet "github.com/meigma/yacd/internal/cardano/wallet"
	corev1 "k8s.io/api/core/v1"
)

const (
	// addressHRP is the bech32 human-readable prefix for testnet addresses.
	addressHRP = "addr_test"

	// addressPrefix is the leading marker that distinguishes a raw bech32
	// address selector from a name or pubkey selector.
	addressPrefix = addressHRP + "1"

	// pubKeyHexLength is the hex character count of a 32-byte ed25519 public key.
	pubKeyHexLength = 64
)

// resolveByName returns the address of the managed wallet whose name label
// equals name. A name matching no wallet is reported as not found.
func resolveByName(secrets []corev1.Secret, name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range secrets {
		if strings.EqualFold(secrets[i].Labels[NameLabel], name) {
			address := string(secrets[i].Data[AddressKey])
			if address == "" {
				return "", fmt.Errorf("wallet %q has no address", name)
			}
			return address, nil
		}
	}

	return "", fmt.Errorf("wallet %q not found", name)
}

// resolveByPubKey returns the address of the managed wallet whose verification
// key decodes to pubKeyHex. A pubkey matching no wallet is reported as not
// found.
func resolveByPubKey(secrets []corev1.Secret, pubKeyHex string) (string, error) {
	for i := range secrets {
		envelope, ok := secrets[i].Data[VerificationKeyKey]
		if !ok {
			continue
		}
		decoded, err := domainwallet.DecodePaymentKeyEnvelope(envelope)
		if err != nil {
			continue
		}
		if decoded == pubKeyHex {
			address := string(secrets[i].Data[AddressKey])
			if address == "" {
				return "", fmt.Errorf("wallet with pubkey %s has no address", pubKeyHex)
			}
			return address, nil
		}
	}

	return "", fmt.Errorf("no managed wallet matches pubkey %s", pubKeyHex)
}

// normalizePubKeyHex reports whether selector is a 32-byte hex public key and,
// if so, returns it lowercased. A selector that is the right length but not hex
// is not a pubkey and is treated as a (failing) name lookup by the caller.
func normalizePubKeyHex(selector string) (string, bool) {
	if len(selector) != pubKeyHexLength {
		return "", false
	}
	if _, err := hex.DecodeString(selector); err != nil {
		return "", false
	}

	return strings.ToLower(selector), true
}

// validateTestnetAddress validates a bech32 Cardano testnet payment address so a
// raw-address selector cannot fund a malformed or mainnet destination.
func validateTestnetAddress(address string) error {
	if strings.TrimSpace(address) != address {
		return errors.New("address must not contain leading or trailing whitespace")
	}
	if strings.ContainsFunc(address, func(char rune) bool {
		return unicode.IsControl(char) || unicode.IsSpace(char)
	}) {
		return errors.New("address must not contain whitespace or control characters")
	}

	hrp, data, err := apolloBech32.Decode(address)
	if err != nil {
		return fmt.Errorf("address is not valid bech32: %w", err)
	}
	if hrp != addressHRP {
		return fmt.Errorf("address human-readable prefix must be %q", addressHRP)
	}

	payload, err := apolloBech32.ConvertBits(data, 5, 8, false)
	if err != nil {
		return fmt.Errorf("address payload is invalid: %w", err)
	}
	if len(payload) == 0 {
		return errors.New("address payload is empty")
	}
	if header := payload[0]; header&0x0f != apolloAddress.TESTNET {
		return errors.New("address network must be testnet")
	}

	return nil
}
