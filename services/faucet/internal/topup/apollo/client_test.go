package apollo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	apolloAddress "github.com/Salvionied/apollo/serialization/Address"
	apolloTx "github.com/Salvionied/apollo/serialization/Transaction"
	"github.com/Salvionied/apollo/serialization/TransactionBody"
	"github.com/Salvionied/apollo/serialization/TransactionInput"
	"github.com/Salvionied/apollo/serialization/TransactionOutput"
	apolloUTxO "github.com/Salvionied/apollo/serialization/UTxO"
	"github.com/Salvionied/apollo/serialization/Value"
	"github.com/SundaeSwap-finance/ogmigo/v6"
	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/meigma/yacd/services/faucet/internal/topup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testSourceSigningHex        = strings.Repeat("01", 32)
	testSourceVerificationHex   = deriveTestVerificationKeyHex(testSourceSigningHex)
	testSourceAddress           = mustDeriveTestnetPaymentAddress(testSourceVerificationHex)
	testDestinationSigningHex   = strings.Repeat("02", 32)
	testDestinationKeyHex       = deriveTestVerificationKeyHex(testDestinationSigningHex)
	testDestinationAddress      = mustDeriveTestnetPaymentAddress(testDestinationKeyHex)
	testTransactionIDBytes      = bytes.Repeat([]byte{0x03}, 32)
	testOtherTransactionIDBytes = bytes.Repeat([]byte{0x04}, 32)
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
			name: "destination equals source",
			mutate: func(request *topup.ChainRequest) {
				request.DestinationAddress = request.Source.Address
			},
			wantErr: "destination address must not equal source address",
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

func TestFilterExcludedUTxOs(t *testing.T) {
	t.Parallel()

	first := testUTxO(testTransactionIDBytes, 0)
	second := testUTxO(testOtherTransactionIDBytes, 1)

	filtered := filterExcludedUTxOs([]apolloUTxO.UTxO{first, second}, []string{first.GetKey()})

	require.Len(t, filtered, 1)
	assert.Equal(t, second.GetKey(), filtered[0].GetKey())
}

func TestValidateTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tx          *apolloTx.Transaction
		sourceUTxOs []apolloUTxO.UTxO
		amount      int64
		wantErr     string
	}{
		{
			name:        "exact output",
			tx:          testTransaction(t, testDestinationAddress),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 1_000_001)},
			amount:      1_000_000,
		},
		{
			name: "allows source change",
			tx: testTransactionWithOutputs(t,
				testOutput(t, testDestinationAddress, 1_000_000),
				testOutput(t, testSourceAddress, 2_000_000),
			),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 3_000_001)},
			amount:      1_000_000,
		},
		{
			name:        "mutated amount",
			tx:          testTransaction(t, testDestinationAddress),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 1_000_001)},
			amount:      999_999,
			wantErr:     "changed destination lovelace",
		},
		{
			name:        "missing destination",
			tx:          testTransaction(t, testSourceAddress),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 1_000_001)},
			amount:      1_000_000,
			wantErr:     "created 0 destination outputs",
		},
		{
			name: "unexpected non-source output",
			tx: testTransactionWithOutputs(t,
				testOutput(t, testDestinationAddress, 1_000_000),
				testOutput(t, mustDeriveTestnetPaymentAddress(deriveTestVerificationKeyHex(strings.Repeat("05", 32))), 1_000_000),
			),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 2_000_001)},
			amount:      1_000_000,
			wantErr:     "created an unexpected output",
		},
		{
			name: "oversized fee",
			tx: testTransactionWithBody(
				[]TransactionInput.TransactionInput{testInput(testTransactionIDBytes, 0)},
				[]TransactionOutput.TransactionOutput{testOutput(t, testDestinationAddress, 1_000_000)},
				2_000_000,
			),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 3_000_000)},
			amount:      1_000_000,
			wantErr:     "fee 2000000 exceeds maximum",
		},
		{
			name: "unknown input",
			tx: testTransactionWithBody(
				[]TransactionInput.TransactionInput{testInput(testOtherTransactionIDBytes, 0)},
				[]TransactionOutput.TransactionOutput{testOutput(t, testDestinationAddress, 1_000_000)},
				1,
			),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 1_000_001)},
			amount:      1_000_000,
			wantErr:     "spent non-source input",
		},
		{
			name:        "excess source loss",
			tx:          testTransaction(t, testDestinationAddress),
			sourceUTxOs: []apolloUTxO.UTxO{testSourceUTxO(t, testTransactionIDBytes, 3_000_000)},
			amount:      1_000_000,
			wantErr:     "consumed 3000000 source lovelace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateTransaction(tt.tx, testDestinationAddress, testSourceAddress, tt.amount, tt.sourceUTxOs, defaultMaxFeeLovelace)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assertTopUpCode(t, err, topup.CodeChainUnavailable)
		})
	}
}

func TestSubmitSignedTransactionHandlesOgmiosResponse(t *testing.T) {
	t.Parallel()

	t.Run("success with empty response id", func(t *testing.T) {
		t.Parallel()

		tx := testTransaction(t, testDestinationAddress)
		expectedID, err := tx.TransactionBody.Id()
		require.NoError(t, err)
		submitter := &fakeOgmiosSubmitter{
			response: &ogmigo.SubmitTxResponse{},
		}

		txID, err := Client{submitter: submitter}.submitSignedTransaction(context.Background(), tx)

		require.NoError(t, err)
		assert.Equal(t, hex.EncodeToString(expectedID.Payload), txID)
		assert.NotEmpty(t, submitter.data)
	})

	t.Run("success with matching response id", func(t *testing.T) {
		t.Parallel()

		tx := testTransaction(t, testDestinationAddress)
		expectedID, err := tx.TransactionBody.Id()
		require.NoError(t, err)
		submitter := &fakeOgmiosSubmitter{
			response: &ogmigo.SubmitTxResponse{ID: strings.ToUpper(hex.EncodeToString(expectedID.Payload))},
		}

		txID, err := Client{submitter: submitter}.submitSignedTransaction(context.Background(), tx)

		require.NoError(t, err)
		assert.Equal(t, hex.EncodeToString(expectedID.Payload), txID)
		assert.NotEmpty(t, submitter.data)
	})

	t.Run("mismatched response id", func(t *testing.T) {
		t.Parallel()

		_, err := Client{
			submitter: &fakeOgmiosSubmitter{
				response: &ogmigo.SubmitTxResponse{ID: strings.Repeat("0", 64)},
			},
		}.submitSignedTransaction(context.Background(), testTransaction(t, testDestinationAddress))

		require.Error(t, err)
		assert.ErrorContains(t, err, "returned transaction id")
		assertTopUpCode(t, err, topup.CodeChainUnavailable)
	})

	t.Run("protocol rejection", func(t *testing.T) {
		t.Parallel()

		submitter := &fakeOgmiosSubmitter{
			response: &ogmigo.SubmitTxResponse{
				Error: &ogmigo.SubmitTxError{Code: 3117, Message: "ValueNotConserved"},
			},
		}

		_, err := Client{submitter: submitter}.submitSignedTransaction(
			context.Background(),
			testTransaction(t, testDestinationAddress),
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, "rejected by Ogmios: code 3117")
		assertTopUpCode(t, err, topup.CodeChainUnavailable)
	})

	t.Run("transport failure", func(t *testing.T) {
		t.Parallel()

		submitter := &fakeOgmiosSubmitter{err: errors.New("websocket failed")}

		_, err := Client{submitter: submitter}.submitSignedTransaction(
			context.Background(),
			testTransaction(t, testDestinationAddress),
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, "submit top-up transaction to Ogmios")
		assertTopUpCode(t, err, topup.CodeChainUnavailable)
	})
}

func testChainRequest() topup.ChainRequest {
	return topup.ChainRequest{
		Source:             testFundingSource(),
		DestinationAddress: testDestinationAddress,
		Lovelace:           1_000_000,
	}
}

func testFundingSource() sources.FundingSource {
	return sources.FundingSource{
		Name:               "utxo1",
		Address:            testSourceAddress,
		VerificationKeyHex: testSourceVerificationHex,
		SigningKeyHex:      testSourceSigningHex,
	}
}

func testTransaction(t *testing.T, destinationAddress string) *apolloTx.Transaction {
	t.Helper()

	return testTransactionWithOutputs(t, testOutput(t, destinationAddress, 1_000_000))
}

func testTransactionWithOutputs(t *testing.T, outputs ...TransactionOutput.TransactionOutput) *apolloTx.Transaction {
	t.Helper()

	return testTransactionWithBody(
		[]TransactionInput.TransactionInput{testInput(testTransactionIDBytes, 0)},
		outputs,
		1,
	)
}

func testTransactionWithBody(
	inputs []TransactionInput.TransactionInput,
	outputs []TransactionOutput.TransactionOutput,
	fee int64,
) *apolloTx.Transaction {
	return &apolloTx.Transaction{
		TransactionBody: TransactionBody.TransactionBody{
			Inputs:  inputs,
			Outputs: outputs,
			Fee:     fee,
		},
		Valid: true,
	}
}

func testOutput(t *testing.T, destinationAddress string, lovelace int64) TransactionOutput.TransactionOutput {
	t.Helper()

	address, err := apolloAddress.DecodeAddress(destinationAddress)
	require.NoError(t, err)

	return TransactionOutput.SimpleTransactionOutput(address, Value.SimpleValue(lovelace, nil))
}

func testUTxO(transactionID []byte, index int) apolloUTxO.UTxO {
	return apolloUTxO.UTxO{
		Input:  testInput(transactionID, index),
		Output: testOutputNoT(testSourceAddress, 2_000_000),
	}
}

func testSourceUTxO(t *testing.T, transactionID []byte, lovelace int64) apolloUTxO.UTxO {
	t.Helper()

	return apolloUTxO.UTxO{
		Input:  testInput(transactionID, 0),
		Output: testOutput(t, testSourceAddress, lovelace),
	}
}

func testInput(transactionID []byte, index int) TransactionInput.TransactionInput {
	return TransactionInput.TransactionInput{
		TransactionId: transactionID,
		Index:         index,
	}
}

func testOutputNoT(destinationAddress string, lovelace int64) TransactionOutput.TransactionOutput {
	address, err := apolloAddress.DecodeAddress(destinationAddress)
	if err != nil {
		panic(err)
	}

	return TransactionOutput.SimpleTransactionOutput(address, Value.SimpleValue(lovelace, nil))
}

type fakeOgmiosSubmitter struct {
	response *ogmigo.SubmitTxResponse
	err      error
	data     string
}

func (f *fakeOgmiosSubmitter) SubmitTx(_ context.Context, data string) (*ogmigo.SubmitTxResponse, error) {
	f.data = data
	if f.err != nil {
		return nil, f.err
	}

	return f.response, nil
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
