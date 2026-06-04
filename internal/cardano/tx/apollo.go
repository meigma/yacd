package tx

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	apolloapi "github.com/Salvionied/apollo"
	"github.com/Salvionied/apollo/constants"
	apolloTx "github.com/Salvionied/apollo/serialization/Transaction"
	"github.com/Salvionied/apollo/serialization/TransactionInput"
	apolloUTxO "github.com/Salvionied/apollo/serialization/UTxO"
	"github.com/Salvionied/apollo/txBuilding/Backend/OgmiosChainContext"
	"github.com/SundaeSwap-finance/kugo"
	"github.com/SundaeSwap-finance/ogmigo/v6"
)

const (
	defaultRequestTimeout = 15 * time.Second
	defaultTTLSlots       = 300
	defaultMaxFeeLovelace = 1_000_000
)

// Apollo is an Apollo/Ogmios/Kupo-backed Submitter.
//
// It builds the transaction with Apollo, queries source UTxOs and the latest
// slot through Ogmios, validates the completed transaction, signs it, and
// submits it via Ogmios. Apollo's chain context uses Kupo for UTxO resolution.
type Apollo struct {
	// OgmiosURL is the websocket endpoint used for chain queries and submission.
	OgmiosURL string
	// KupoURL is the HTTP endpoint used by Apollo's Ogmios chain context.
	KupoURL string
	// RequestTimeout bounds each chain request. Zero selects a default.
	RequestTimeout time.Duration
	// TTLSlots is the transaction validity window measured from the latest slot.
	TTLSlots int64
	// MaxFeeLovelace bounds the fee a completed transaction may pay. Zero
	// selects a default.
	MaxFeeLovelace int64

	// submitter is injected for testing; nil uses a live Ogmios client built
	// from OgmiosURL.
	submitter ogmiosSubmitter
}

type ogmiosSubmitter interface {
	SubmitTx(ctx context.Context, data string) (*ogmigo.SubmitTxResponse, error)
}

// Submit submits one funding transaction with Apollo, Ogmios, and Kupo.
func (c Apollo) Submit(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, WrapError(CodeChainUnavailable, "submit funding transaction with Apollo: context canceled", err)
	}
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	chainContext, err := c.newChainContext(ctx)
	if err != nil {
		return Result{}, err
	}
	sourceUTxOs, err := c.sourceUTxOs(ctx, request.SourceAddress)
	if err != nil {
		return Result{}, err
	}
	if len(sourceUTxOs) == 0 {
		return Result{}, Errorf(
			CodeChainUnavailable,
			"submit funding transaction with Apollo: source %q has no spendable UTxOs",
			request.SourceName,
		)
	}
	sourceUTxOs = filterExcludedUTxOs(sourceUTxOs, request.ExcludeInputKeys)
	if len(sourceUTxOs) == 0 {
		return Result{}, Errorf(
			CodeChainUnavailable,
			"submit funding transaction with Apollo: source %q has no available UTxOs after pending submissions; retry after the chain state advances",
			request.SourceName,
		)
	}

	txID, spentInputKeys, err := c.submit(ctx, chainContext, request, sourceUTxOs)
	if err != nil {
		return Result{}, err
	}

	return Result{TxID: txID, SpentInputKeys: spentInputKeys}, nil
}

func (c Apollo) newChainContext(ctx context.Context) (*OgmiosChainContext.OgmiosChainContext, error) {
	if strings.TrimSpace(c.OgmiosURL) == "" {
		return nil, Errorf(CodeChainUnavailable, "submit funding transaction with Apollo: Ogmios URL is required")
	}
	if strings.TrimSpace(c.KupoURL) == "" {
		return nil, Errorf(CodeChainUnavailable, "submit funding transaction with Apollo: Kupo URL is required")
	}

	ogmiosClient := ogmigo.New(ogmigo.WithEndpoint(c.OgmiosURL))
	kupoClient := kugo.New(kugo.WithEndpoint(c.KupoURL))
	chainContext := OgmiosChainContext.NewOgmiosChainContext(ogmiosClient, kupoClient)
	chainContext.BaseContext = ctx
	chainContext.RequestTimeout = c.requestTimeout()
	if err := chainContext.Init(); err != nil {
		return nil, WrapError(CodeChainUnavailable, "initialize Apollo Ogmios chain context", err)
	}

	return &chainContext, nil
}

func (c Apollo) sourceUTxOs(ctx context.Context, address string) ([]apolloUTxO.UTxO, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()

	ogmiosClient := ogmigo.New(ogmigo.WithEndpoint(c.OgmiosURL))
	utxos, err := ogmiosClient.UtxosByAddress(requestCtx, address)
	if err != nil {
		return nil, WrapError(CodeChainUnavailable, "query source UTxOs from Ogmios", err)
	}

	results := make([]apolloUTxO.UTxO, 0, len(utxos))
	for _, utxo := range utxos {
		apolloUTxOValue, err := OgmiosChainContext.Utxo_OgmigoToApollo(utxo)
		if err != nil {
			return nil, WrapError(CodeChainUnavailable, "convert Ogmios UTxO to Apollo UTxO", err)
		}
		results = append(results, apolloUTxOValue)
	}

	return results, nil
}

func (c Apollo) submit(
	ctx context.Context,
	chainContext *OgmiosChainContext.OgmiosChainContext,
	request Request,
	sourceUTxOs []apolloUTxO.UTxO,
) (string, []string, error) {
	vkey, err := validateRawKeyHex(request.SourceName, "verification", request.VerificationKeyHex)
	if err != nil {
		return "", nil, err
	}
	skey, err := validateRawKeyHex(request.SourceName, "signing", request.SigningKeyHex)
	if err != nil {
		return "", nil, err
	}
	lovelace, err := intLovelace(request.Lovelace)
	if err != nil {
		return "", nil, err
	}

	builder := apolloapi.New(chainContext).SetWalletFromKeypair(vkey, skey, constants.TESTNET)
	builder = builder.AddLoadedUTxOs(sourceUTxOs...)
	builder, err = builder.SetWalletAsChangeAddress()
	if err != nil {
		return "", nil, WrapError(CodeChainUnavailable, "set funding source as change address", err)
	}

	slot, err := chainContext.LastBlockSlot()
	if err != nil {
		return "", nil, WrapError(CodeChainUnavailable, "read latest block slot", err)
	}
	builder.
		SetValidityStart(int64(slot)).
		SetTtl(int64(slot)+c.ttlSlots()).
		PayToAddressBech32(request.DestinationAddress, lovelace)

	builder, _, err = builder.Complete()
	if err != nil {
		return "", nil, WrapError(CodeChainUnavailable, "complete funding transaction", err)
	}
	if err := validateTransaction(
		builder.GetTx(),
		request.DestinationAddress,
		request.SourceAddress,
		request.Lovelace,
		sourceUTxOs,
		c.maxFeeLovelace(),
	); err != nil {
		return "", nil, err
	}
	builder.Sign()
	transaction := builder.GetTx()
	spentKeys := spentInputKeys(transaction.TransactionBody.Inputs)
	if len(spentKeys) == 0 {
		return "", nil, Errorf(CodeChainUnavailable, "submit funding transaction spent no source inputs")
	}

	txID, err := c.submitSignedTransaction(ctx, transaction)
	if err != nil {
		return "", nil, err
	}

	return txID, spentKeys, nil
}

func filterExcludedUTxOs(utxos []apolloUTxO.UTxO, excludedInputKeys []string) []apolloUTxO.UTxO {
	if len(utxos) == 0 || len(excludedInputKeys) == 0 {
		return utxos
	}

	excluded := make(map[string]struct{}, len(excludedInputKeys))
	for _, inputKey := range excludedInputKeys {
		inputKey = strings.ToLower(strings.TrimSpace(inputKey))
		if inputKey == "" {
			continue
		}
		excluded[inputKey] = struct{}{}
	}
	if len(excluded) == 0 {
		return utxos
	}

	filtered := make([]apolloUTxO.UTxO, 0, len(utxos))
	for _, utxo := range utxos {
		if _, ok := excluded[strings.ToLower(utxo.GetKey())]; ok {
			continue
		}
		filtered = append(filtered, utxo)
	}

	return filtered
}

func validateTransaction(
	transaction *apolloTx.Transaction,
	destinationAddress string,
	sourceAddress string,
	lovelace int64,
	sourceUTxOs []apolloUTxO.UTxO,
	maxFeeLovelace int64,
) error {
	if transaction == nil {
		return Errorf(CodeChainUnavailable, "complete funding transaction returned nil transaction")
	}
	if transaction.TransactionBody.Fee < 0 {
		return Errorf(CodeChainUnavailable, "complete funding transaction created a negative fee")
	}
	if transaction.TransactionBody.Fee > maxFeeLovelace {
		return Errorf(
			CodeChainUnavailable,
			"complete funding transaction fee %d exceeds maximum %d",
			transaction.TransactionBody.Fee,
			maxFeeLovelace,
		)
	}
	if lovelace > math.MaxInt64-transaction.TransactionBody.Fee {
		return Errorf(CodeChainUnavailable, "complete funding transaction lovelace and fee overflow")
	}

	sourceByInput := make(map[string]apolloUTxO.UTxO, len(sourceUTxOs))
	for _, sourceUTxO := range sourceUTxOs {
		if sourceUTxO.Output.GetAddress().String() != sourceAddress {
			return Errorf(
				CodeChainUnavailable,
				"source UTxO %s has unexpected address %s",
				sourceUTxO.GetKey(),
				sourceUTxO.Output.GetAddress().String(),
			)
		}
		sourceByInput[strings.ToLower(sourceUTxO.GetKey())] = sourceUTxO
	}
	if len(transaction.TransactionBody.Inputs) == 0 {
		return Errorf(CodeChainUnavailable, "complete funding transaction spent no source inputs")
	}

	matches := 0
	sourceInputLovelace := int64(0)
	sourceChangeLovelace := int64(0)
	for _, input := range transaction.TransactionBody.Inputs {
		sourceUTxO, ok := sourceByInput[strings.ToLower(inputKey(input))]
		if !ok {
			return Errorf(
				CodeChainUnavailable,
				"complete funding transaction spent non-source input %s",
				inputKey(input),
			)
		}
		sourceInputLovelace += sourceUTxO.Output.Lovelace()
	}
	for _, output := range transaction.TransactionBody.Outputs {
		outputAddress := output.GetAddress().String()
		if outputAddress == sourceAddress {
			sourceChangeLovelace += output.Lovelace()
			continue
		}
		if outputAddress != destinationAddress {
			return Errorf(
				CodeChainUnavailable,
				"complete funding transaction created an unexpected output to %s",
				outputAddress,
			)
		}

		matches++
		if output.Lovelace() != lovelace {
			return Errorf(
				CodeChainUnavailable,
				"complete funding transaction changed destination lovelace from %d to %d",
				lovelace,
				output.Lovelace(),
			)
		}
		if !output.GetValue().GetAssets().RemoveZeroAssets().IsEmpty() {
			return Errorf(CodeChainUnavailable, "complete funding transaction added assets to destination output")
		}
	}
	if matches != 1 {
		return Errorf(
			CodeChainUnavailable,
			"complete funding transaction created %d destination outputs, want 1",
			matches,
		)
	}
	sourceLoss := sourceInputLovelace - sourceChangeLovelace
	expectedLoss := lovelace + transaction.TransactionBody.Fee
	if sourceLoss != expectedLoss {
		return Errorf(
			CodeChainUnavailable,
			"complete funding transaction consumed %d source lovelace, want %d",
			sourceLoss,
			expectedLoss,
		)
	}

	return nil
}

func spentInputKeys(inputs []TransactionInput.TransactionInput) []string {
	result := make([]string, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, inputKey(input))
	}

	return result
}

func inputKey(input TransactionInput.TransactionInput) string {
	return fmt.Sprintf("%s:%d", hex.EncodeToString(input.TransactionId), input.Index)
}

func (c Apollo) submitSignedTransaction(ctx context.Context, transaction *apolloTx.Transaction) (string, error) {
	if transaction == nil {
		return "", Errorf(CodeChainUnavailable, "submit funding transaction: transaction is nil")
	}

	txBytes, err := transaction.Bytes()
	if err != nil {
		return "", WrapError(CodeChainUnavailable, "encode funding transaction", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()

	response, err := c.ogmiosSubmitter().SubmitTx(requestCtx, hex.EncodeToString(txBytes))
	if err != nil {
		return "", WrapError(CodeChainUnavailable, "submit funding transaction to Ogmios", err)
	}
	if response == nil {
		return "", Errorf(CodeChainUnavailable, "submit funding transaction to Ogmios returned no response")
	}
	if response.Error != nil {
		return "", Errorf(
			CodeChainUnavailable,
			"submit funding transaction rejected by Ogmios: code %d: %s",
			response.Error.Code,
			strings.TrimSpace(response.Error.Message),
		)
	}

	txID, err := transaction.TransactionBody.Id()
	if err != nil {
		return "", WrapError(CodeChainUnavailable, "compute submitted transaction id", err)
	}
	txIDHex := hex.EncodeToString(txID.Payload)
	if responseID := strings.ToLower(strings.TrimSpace(response.ID)); responseID != "" && responseID != txIDHex {
		return "", Errorf(
			CodeChainUnavailable,
			"submit funding transaction to Ogmios returned transaction id %q, want %q",
			response.ID,
			txIDHex,
		)
	}

	return txIDHex, nil
}

func (c Apollo) ogmiosSubmitter() ogmiosSubmitter {
	if c.submitter != nil {
		return c.submitter
	}

	return ogmigo.New(ogmigo.WithEndpoint(c.OgmiosURL))
}

func validateRawKeyHex(name string, kind string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return "", WrapError(
			CodeInvalidRequest,
			fmt.Sprintf("decode source %q %s key hex", name, kind),
			err,
		)
	}
	if len(decoded) != rawKeyLength {
		return "", Errorf(
			CodeInvalidRequest,
			"decode source %q %s key hex: expected %d bytes, got %d",
			name,
			kind,
			rawKeyLength,
			len(decoded),
		)
	}

	return strings.ToLower(trimmed), nil
}

func intLovelace(value int64) (int, error) {
	amount, err := strconv.Atoi(strconv.FormatInt(value, 10))
	if err != nil {
		return 0, WrapError(
			CodeInvalidRequest,
			fmt.Sprintf("lovelace %d cannot be represented as int", value),
			err,
		)
	}

	return amount, nil
}

func (c Apollo) requestTimeout() time.Duration {
	if c.RequestTimeout <= 0 {
		return defaultRequestTimeout
	}

	return c.RequestTimeout
}

func (c Apollo) ttlSlots() int64 {
	if c.TTLSlots <= 0 {
		return defaultTTLSlots
	}

	return c.TTLSlots
}

func (c Apollo) maxFeeLovelace() int64 {
	if c.MaxFeeLovelace <= 0 {
		return defaultMaxFeeLovelace
	}

	return c.MaxFeeLovelace
}
