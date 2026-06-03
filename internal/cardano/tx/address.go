package tx

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	apolloBech32 "github.com/Salvionied/apollo/crypto/bech32"
	apolloAddress "github.com/Salvionied/apollo/serialization/Address"

	"github.com/meigma/yacd/internal/cardano/wallet"
)

const (
	rawKeyLength            = 32
	keyHashBytesLength      = 28
	baseAddressBytesLength  = 1 + keyHashBytesLength + keyHashBytesLength
	shortAddressBytesLength = 1 + keyHashBytesLength
	addressHRP              = "addr_test"
	addressPrefix           = addressHRP + "1"
)

// ValidateTestnetAddress validates a bech32 Cardano testnet payment address.
func ValidateTestnetAddress(address string) error {
	if strings.TrimSpace(address) != address {
		return errors.New("address must not contain leading or trailing whitespace")
	}
	if address == "" {
		return errors.New("address is required")
	}
	if strings.ContainsFunc(address, func(char rune) bool {
		return unicode.IsControl(char) || unicode.IsSpace(char)
	}) {
		return errors.New("address must not contain whitespace or control characters")
	}
	if !strings.HasPrefix(address, addressPrefix) {
		return fmt.Errorf("address must start with %q", addressPrefix)
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

	header := payload[0]
	network := header & 0x0f
	addressType := (header & 0xf0) >> 4
	if network != apolloAddress.TESTNET {
		return errors.New("address network must be testnet")
	}
	switch addressType {
	case apolloAddress.KEY_KEY, apolloAddress.SCRIPT_KEY, apolloAddress.KEY_SCRIPT, apolloAddress.SCRIPT_SCRIPT:
		if len(payload) != baseAddressBytesLength {
			return fmt.Errorf("address payload length is %d bytes, want %d", len(payload), baseAddressBytesLength)
		}
	case apolloAddress.KEY_NONE, apolloAddress.SCRIPT_NONE:
		if len(payload) != shortAddressBytesLength {
			return fmt.Errorf("address payload length is %d bytes, want %d", len(payload), shortAddressBytesLength)
		}
	default:
		return errors.New("address must be a payment address")
	}

	return nil
}

// DeriveTestnetPaymentAddress returns the enterprise testnet address for a raw
// 32-byte payment verification key encoded as lowercase or uppercase hex.
//
// It shares wallet.DeriveTestnetAddress so the engine, the operator, and any
// key store can never compute an address two different ways.
func DeriveTestnetPaymentAddress(verificationKeyHex string) (string, error) {
	verificationKeyBytes, err := decodeRawKeyHex(verificationKeyHex, "verification key")
	if err != nil {
		return "", err
	}

	return wallet.DeriveTestnetAddress(verificationKeyBytes)
}

// validateSourceKeys validates that the request's source key pair is well
// formed, internally consistent, and matches the source address.
func validateSourceKeys(request Request) error {
	if err := ValidateTestnetAddress(request.SourceAddress); err != nil {
		return fmt.Errorf("source address is invalid: %w", err)
	}
	verificationKeyBytes, err := decodeRawKeyHex(request.VerificationKeyHex, "verification key")
	if err != nil {
		return err
	}
	signingKeyBytes, err := decodeRawKeyHex(request.SigningKeyHex, "signing key")
	if err != nil {
		return err
	}

	signingKey := ed25519.NewKeyFromSeed(signingKeyBytes)
	signingPublicKey, ok := signingKey.Public().(ed25519.PublicKey)
	if !ok {
		return errors.New("signing key cannot derive a public key")
	}
	if !bytes.Equal(verificationKeyBytes, signingPublicKey) {
		return errors.New("signing key does not match verification key")
	}

	derivedAddress, err := wallet.DeriveTestnetAddress(verificationKeyBytes)
	if err != nil {
		return fmt.Errorf("derive source address: %w", err)
	}
	if request.SourceAddress != derivedAddress {
		return errors.New("source address does not match verification key")
	}

	return nil
}

func decodeRawKeyHex(value string, fieldName string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decode %s hex: %w", fieldName, err)
	}
	if len(decoded) != rawKeyLength {
		return nil, fmt.Errorf("%s must be %d bytes, got %d", fieldName, rawKeyLength, len(decoded))
	}

	return decoded, nil
}
