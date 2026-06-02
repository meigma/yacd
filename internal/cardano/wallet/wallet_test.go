package wallet

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goldenSeedHex and goldenAddress are an external correctness anchor: the
// address was produced by `cardano-cli address build --testnet-magic 42
// --payment-verification-key-file <vkey>` for the verification key derived
// from this fixed seed. If this assertion ever breaks, the derivation drifted
// from cardano-cli and would send funds to the wrong address — fix the
// derivation, do not update the golden.
const (
	goldenSeedHex = "0101010101010101010101010101010101010101010101010101010101010101"
	goldenPubHex  = "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"
	goldenAddress = "addr_test1vqxk54m7j3q6mrkevcunryrwf4p7e68c93cjk8gzxkhlkpsffv7s0"
	cborByteStr32 = "5820" // CBOR header for a 32-byte byte string
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)

	return b
}

func envelopeCBORHex(t *testing.T, envelope []byte) (keyType, cborHex string) {
	t.Helper()
	var decoded textEnvelope
	require.NoError(t, json.Unmarshal(envelope, &decoded))

	return decoded.Type, decoded.CBORHex
}

func TestFromSeedMatchesCardanoCLIGolden(t *testing.T) {
	material, err := FromSeed(mustHex(t, goldenSeedHex))
	require.NoError(t, err)

	assert.Equal(t, goldenAddress, material.Address,
		"derived address must match the cardano-cli golden for the fixed seed")

	skeyType, skeyCBOR := envelopeCBORHex(t, material.SigningKeyEnvelope)
	assert.Equal(t, signingKeyType, skeyType)
	assert.Equal(t, cborByteStr32+goldenSeedHex, skeyCBOR,
		"signing key envelope must wrap the raw seed")

	vkeyType, vkeyCBOR := envelopeCBORHex(t, material.VerificationKeyEnvelope)
	assert.Equal(t, verificationKeyType, vkeyType)
	assert.Equal(t, cborByteStr32+goldenPubHex, vkeyCBOR,
		"verification key envelope must wrap the derived public key")

	// Envelopes are valid JSON with a trailing newline, like cardano-cli output.
	assert.True(t, strings.HasSuffix(string(material.SigningKeyEnvelope), "}\n"))
}

func TestFromSeedSigningKeyDerivesAddress(t *testing.T) {
	// The persisted signing key must, on its own, reconstruct the wallet
	// address — proving the exported key actually controls the funded address.
	material, err := FromSeed(mustHex(t, goldenSeedHex))
	require.NoError(t, err)

	_, skeyCBOR := envelopeCBORHex(t, material.SigningKeyEnvelope)
	seed := decodeEnvelopeKey(t, skeyCBOR)

	public, ok := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	require.True(t, ok)

	address, err := DeriveTestnetAddress(public)
	require.NoError(t, err)
	assert.Equal(t, material.Address, address)
}

func TestNewProducesDistinctValidWallets(t *testing.T) {
	first, err := New()
	require.NoError(t, err)
	second, err := New()
	require.NoError(t, err)

	assert.NotEqual(t, first.Address, second.Address, "each wallet must be unique")
	for _, m := range []Material{first, second} {
		assert.True(t, strings.HasPrefix(m.Address, addressHRP+"1"))
		_, skeyCBOR := envelopeCBORHex(t, m.SigningKeyEnvelope)
		assert.Len(t, decodeEnvelopeKey(t, skeyCBOR), seedLength)
	}
}

func TestFromSeedRejectsWrongLength(t *testing.T) {
	_, err := FromSeed([]byte{0x01, 0x02})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seed must be")
}

func TestDeriveTestnetAddressRejectsWrongLength(t *testing.T) {
	_, err := DeriveTestnetAddress([]byte{0x01})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification key must be")
}

func decodeEnvelopeKey(t *testing.T, cborHex string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(cborHex)
	require.NoError(t, err)
	var key []byte
	require.NoError(t, cbor.Unmarshal(raw, &key))

	return key
}
