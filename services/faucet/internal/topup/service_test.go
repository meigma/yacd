package topup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDestinationAddress = "addr_test1vqy2n0vz5rlpykf6dcqn55xdcpey7mejyexlgj6370leayst4k6ta"
	testSourceAddress      = "addr_test1vqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqs8fu43"
	testVerificationHex    = "0101010101010101010101010101010101010101010101010101010101010101"
	testSigningHex         = "0202020202020202020202020202020202020202020202020202020202020202"
)

func TestServiceSubmitUsesDefaultSource(t *testing.T) {
	t.Parallel()

	reader := &fakeSourceReader{
		defaultName: "utxo1",
		sources: map[string]sources.FundingSource{
			"utxo1": testFundingSource("utxo1"),
		},
	}
	submitter := &fakeSubmitter{result: ChainResult{TxID: "ABC123"}}
	service := NewService(reader, submitter, 10_000_000)

	result, err := service.Submit(context.Background(), Request{
		DestinationAddress: testDestinationAddress,
		Lovelace:           1_000_000,
	})

	require.NoError(t, err)
	assert.Equal(t, "utxo1", reader.names[0])
	require.Len(t, submitter.requests, 1)
	assert.Equal(t, "utxo1", submitter.requests[0].Source.Name)
	assert.Equal(t, testDestinationAddress, submitter.requests[0].DestinationAddress)
	assert.Equal(t, int64(1_000_000), submitter.requests[0].Lovelace)
	assert.Equal(t, "abc123", result.TxID)
	assert.Equal(t, "utxo1", result.Source)
	assert.Equal(t, testSourceAddress, result.SourceAddress)
	assert.Equal(t, testDestinationAddress, result.DestinationAddress)
	assert.Equal(t, int64(1_000_000), result.Lovelace)
}

func TestServiceSubmitUsesSelectedSource(t *testing.T) {
	t.Parallel()

	reader := &fakeSourceReader{
		defaultName: "utxo1",
		sources: map[string]sources.FundingSource{
			"utxo2": testFundingSource("utxo2"),
		},
	}
	submitter := &fakeSubmitter{result: ChainResult{TxID: "def456"}}
	service := NewService(reader, submitter, 10_000_000)

	_, err := service.Submit(context.Background(), Request{
		Source:             "utxo2",
		DestinationAddress: testDestinationAddress,
		Lovelace:           2_000_000,
	})

	require.NoError(t, err)
	assert.Equal(t, "utxo2", reader.names[0])
	assert.Equal(t, "utxo2", submitter.requests[0].Source.Name)
}

func TestServiceSubmitRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request Request
	}{
		{
			name: "invalid source",
			request: Request{
				Source:             "wallet1",
				DestinationAddress: testDestinationAddress,
				Lovelace:           1,
			},
		},
		{
			name: "invalid address",
			request: Request{
				DestinationAddress: "addr1qx2fxv2umyhttkxyxp8x0dlpdt3k6cwng5pxj3l62x5n0x",
				Lovelace:           1,
			},
		},
		{
			name: "zero lovelace",
			request: Request{
				DestinationAddress: testDestinationAddress,
			},
		},
		{
			name: "negative lovelace",
			request: Request{
				DestinationAddress: testDestinationAddress,
				Lovelace:           -1,
			},
		},
		{
			name: "over max",
			request: Request{
				DestinationAddress: testDestinationAddress,
				Lovelace:           10_000_001,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(&fakeSourceReader{defaultName: "utxo1"}, &fakeSubmitter{}, 10_000_000)

			_, err := service.Submit(context.Background(), tt.request)

			require.Error(t, err)
			assertTopUpCode(t, err, CodeInvalidRequest)
		})
	}
}

func TestServiceSubmitMapsSourceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sourceErr error
		wantCode  string
	}{
		{
			name:      "not found",
			sourceErr: &sources.Error{Code: sources.CodeSourceNotFound, Message: "missing"},
			wantCode:  CodeSourceNotFound,
		},
		{
			name:      "unavailable",
			sourceErr: &sources.Error{Code: sources.CodeSourceInvalidKey, Message: "bad key"},
			wantCode:  CodeSourceUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(
				&fakeSourceReader{defaultName: "utxo1", err: tt.sourceErr},
				&fakeSubmitter{},
				10_000_000,
			)

			_, err := service.Submit(context.Background(), Request{
				DestinationAddress: testDestinationAddress,
				Lovelace:           1,
			})

			require.Error(t, err)
			assertTopUpCode(t, err, tt.wantCode)
		})
	}
}

func TestServiceSubmitMapsTransactionFailure(t *testing.T) {
	t.Parallel()

	service := NewService(
		&fakeSourceReader{
			defaultName: "utxo1",
			sources: map[string]sources.FundingSource{
				"utxo1": testFundingSource("utxo1"),
			},
		},
		&fakeSubmitter{err: errors.New("chain failed")},
		10_000_000,
	)

	_, err := service.Submit(context.Background(), Request{
		DestinationAddress: testDestinationAddress,
		Lovelace:           1,
	})

	require.Error(t, err)
	assertTopUpCode(t, err, CodeChainUnavailable)
}

func TestResultJSONDoesNotExposeKeyMaterial(t *testing.T) {
	t.Parallel()

	reader := &fakeSourceReader{
		defaultName: "utxo1",
		sources: map[string]sources.FundingSource{
			"utxo1": testFundingSource("utxo1"),
		},
	}
	service := NewService(reader, &fakeSubmitter{result: ChainResult{TxID: "abc123"}}, 10_000_000)

	result, err := service.Submit(context.Background(), Request{
		DestinationAddress: testDestinationAddress,
		Lovelace:           1,
	})
	require.NoError(t, err)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), testVerificationHex)
	assert.NotContains(t, string(encoded), testSigningHex)
	assert.False(t, strings.Contains(string(encoded), "SigningKey"), string(encoded))
}

func testFundingSource(name string) sources.FundingSource {
	return sources.FundingSource{
		Name:               name,
		Address:            testSourceAddress,
		VerificationKeyHex: testVerificationHex,
		SigningKeyHex:      testSigningHex,
	}
}

type fakeSourceReader struct {
	defaultName string
	sources     map[string]sources.FundingSource
	err         error
	names       []string
}

func (f *fakeSourceReader) DefaultName() string {
	return f.defaultName
}

func (f *fakeSourceReader) ReadFundingSource(_ context.Context, name string) (sources.FundingSource, error) {
	f.names = append(f.names, name)
	if f.err != nil {
		return sources.FundingSource{}, f.err
	}
	source, ok := f.sources[name]
	if !ok {
		return sources.FundingSource{}, &sources.Error{Code: sources.CodeSourceNotFound, Message: "missing"}
	}

	return source, nil
}

type fakeSubmitter struct {
	result   ChainResult
	err      error
	requests []ChainRequest
}

func (f *fakeSubmitter) SubmitTopUp(_ context.Context, request ChainRequest) (ChainResult, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return ChainResult{}, f.err
	}

	return f.result, nil
}

func assertTopUpCode(t *testing.T, err error, code string) {
	t.Helper()

	var topupErr *Error
	require.ErrorAs(t, err, &topupErr)
	assert.Equal(t, code, topupErr.Code)
}
