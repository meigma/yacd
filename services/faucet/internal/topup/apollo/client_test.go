package apollo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/meigma/yacd/services/faucet/internal/topup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testSigningHex      = strings.Repeat("01", 32)
	testVerificationHex = deriveTestVerificationKeyHex(testSigningHex)
	testAddress         = mustDeriveTestnetPaymentAddress(testVerificationHex)
)

func TestSourceKeyAddressDerivesTestnetAddress(t *testing.T) {
	t.Parallel()

	address, err := sourceKeyAddress(testFundingSource())

	require.NoError(t, err)
	assert.NoError(t, sources.ValidateTestnetAddress(address))
	assert.Contains(t, address, "addr_test1")
}

func TestSourceKeyAddressRejectsMalformedKeys(t *testing.T) {
	t.Parallel()

	source := testFundingSource()
	source.VerificationKeyHex = "abcd"

	address, err := sourceKeyAddress(source)

	require.Error(t, err)
	assert.Empty(t, address)
	assertTopUpCode(t, err, topup.CodeInvalidRequest)
}

func TestClientSubmitTopUpRequiresEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client Client
		want   string
	}{
		{
			name: "missing Ogmios",
			client: Client{
				KupoURL: "http://127.0.0.1:1442",
			},
			want: "Ogmios URL is required",
		},
		{
			name: "missing Kupo",
			client: Client{
				OgmiosURL: "ws://127.0.0.1:1337",
			},
			want: "Kupo URL is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.client.SubmitTopUp(context.Background(), testChainRequest())

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			assertTopUpCode(t, err, topup.CodeChainUnavailable)
		})
	}
}

func TestClientSubmitTopUpValidatesRequestBeforeNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*topup.ChainRequest)
		wantErr string
	}{
		{
			name: "missing source name",
			mutate: func(request *topup.ChainRequest) {
				request.Source.Name = ""
			},
			wantErr: "source name is required",
		},
		{
			name: "missing source address",
			mutate: func(request *topup.ChainRequest) {
				request.Source.Address = ""
			},
			wantErr: "invalid source",
		},
		{
			name: "missing destination",
			mutate: func(request *topup.ChainRequest) {
				request.DestinationAddress = ""
			},
			wantErr: "destination address is required",
		},
		{
			name: "zero lovelace",
			mutate: func(request *topup.ChainRequest) {
				request.Lovelace = 0
			},
			wantErr: "lovelace must be positive",
		},
		{
			name: "bad verification key",
			mutate: func(request *topup.ChainRequest) {
				request.Source.VerificationKeyHex = "abcd"
			},
			wantErr: "invalid source",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := testChainRequest()
			tt.mutate(&request)

			_, err := Client{}.SubmitTopUp(context.Background(), request)

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assertTopUpCode(t, err, topup.CodeInvalidRequest)
		})
	}
}

func testChainRequest() topup.ChainRequest {
	return topup.ChainRequest{
		Source:             testFundingSource(),
		DestinationAddress: testAddress,
		Lovelace:           1_000_000,
	}
}

func testFundingSource() sources.FundingSource {
	return sources.FundingSource{
		Name:               "utxo1",
		Address:            testAddress,
		VerificationKeyHex: testVerificationHex,
		SigningKeyHex:      testSigningHex,
	}
}

func assertTopUpCode(t *testing.T, err error, code string) {
	t.Helper()

	var topupErr *topup.Error
	require.ErrorAs(t, err, &topupErr)
	assert.Equal(t, code, topupErr.Code)
}

func deriveTestVerificationKeyHex(signingKeyHex string) string {
	signingKey, err := hex.DecodeString(signingKeyHex)
	if err != nil {
		panic(err)
	}
	privateKey := ed25519.NewKeyFromSeed(signingKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return hex.EncodeToString(publicKey)
}

func mustDeriveTestnetPaymentAddress(verificationKeyHex string) string {
	address, err := sources.DeriveTestnetPaymentAddress(verificationKeyHex)
	if err != nil {
		panic(err)
	}

	return address
}
