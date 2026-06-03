package tx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveTestnetPaymentAddress(t *testing.T) {
	t.Parallel()

	address, err := DeriveTestnetPaymentAddress(testSourceVerificationHex)

	require.NoError(t, err)
	assert.NoError(t, ValidateTestnetAddress(address))
	assert.Contains(t, address, "addr_test1")
}

func TestDeriveTestnetPaymentAddressRejectsMalformedKey(t *testing.T) {
	t.Parallel()

	_, err := DeriveTestnetPaymentAddress("abcd")

	require.Error(t, err)
	assert.ErrorContains(t, err, "verification key")
}

func TestValidateSourceKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Request)
		wantErr string
	}{
		{
			name:   "valid source",
			mutate: func(*Request) {},
		},
		{
			name: "invalid source address",
			mutate: func(request *Request) {
				request.SourceAddress = "not-an-address"
			},
			wantErr: "source address is invalid",
		},
		{
			name: "malformed verification key",
			mutate: func(request *Request) {
				request.VerificationKeyHex = "abcd"
			},
			wantErr: "verification key",
		},
		{
			name: "verification key does not match signing key",
			mutate: func(request *Request) {
				request.VerificationKeyHex = deriveTestVerificationKeyHex(strings.Repeat("07", 32))
			},
			wantErr: "signing key does not match verification key",
		},
		{
			name: "address does not match verification key",
			mutate: func(request *Request) {
				request.SourceAddress = testDestinationAddress
			},
			wantErr: "source address does not match verification key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := testRequest()
			tt.mutate(&request)

			err := validateSourceKeys(request)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
