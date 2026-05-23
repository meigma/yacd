package apollo

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

	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/meigma/yacd/services/faucet/internal/topup"
)

const (
	defaultRequestTimeout = 15 * time.Second
	defaultTTLSlots       = 300
	defaultMaxFeeLovelace = 1_000_000
	rawKeyLength          = 32
)

// Client submits faucet top-up transactions with Apollo, Ogmios, and Kupo.
type Client struct {
	// OgmiosURL is the websocket endpoint used for chain queries and submission.
	OgmiosURL string
	// KupoURL is the HTTP endpoint used by Apollo's Ogmios chain context.
	KupoURL string
	// RequestTimeout bounds each chain request. Zero selects a default.
	RequestTimeout time.Duration
	// TTLSlots is the transaction validity window measured from the latest slot.
	TTLSlots int64
	// MaxFeeLovelace bounds the fee a completed transaction may pay. Zero selects a default.
	MaxFeeLovelace int64

	submitter ogmiosSubmitter
}

type ogmiosSubmitter interface {
	SubmitTx(ctx context.Context, data string) (*ogmigo.SubmitTxResponse, error)
}

// SubmitTopUp submits one exact faucet top-up transaction.
func (c Client) SubmitTopUp(ctx context.Context, request topup.ChainRequest) (topup.ChainResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return topup.ChainResult{}, topup.WrapError(topup.CodeChainUnavailable, "submit top-up with Apollo: context canceled", err)
	}
	if err := validateRequest(request); err != nil {
		return topup.ChainResult{}, err
	}
	chainContext, err := c.newChainContext(ctx)
	if err != nil {
		return topup.ChainResult{}, err
	}
	sourceUTxOs, err := c.sourceUTxOs(ctx, request.Source.Address)
	if err != nil {
		return topup.ChainResult{}, err
	}
	if len(sourceUTxOs) == 0 {
		return topup.ChainResult{}, topup.Errorf(
			topup.CodeChainUnavailable,
			"submit top-up with Apollo: source %q has no spendable UTxOs",
			request.Source.Name,
		)
	}
	sourceUTxOs = filterExcludedUTxOs(sourceUTxOs, request.ExcludeInputKeys)
	if len(sourceUTxOs) == 0 {
		return topup.ChainResult{}, topup.Errorf(
			topup.CodeChainUnavailable,
			"submit top-up with Apollo: source %q has no available UTxOs after pending submissions; retry after the chain state advances",
			request.Source.Name,
		)
	}

	txID, spentInputKeys, err := c.submit(ctx, chainContext, request, sourceUTxOs)
	if err != nil {
		return topup.ChainResult{}, err
	}

	return topup.ChainResult{TxID: txID, SpentInputKeys: spentInputKeys}, nil
}

func (c Client) newChainContext(ctx context.Context) (*OgmiosChainContext.OgmiosChainContext, error) {
	if strings.TrimSpace(c.OgmiosURL) == "" {
		return nil, topup.Errorf(topup.CodeChainUnavailable, "submit top-up with Apollo: Ogmios URL is required")
	}
	if strings.TrimSpace(c.KupoURL) == "" {
		return nil, topup.Errorf(topup.CodeChainUnavailable, "submit top-up with Apollo: Kupo URL is required")
	}

	ogmiosClient := ogmigo.New(ogmigo.WithEndpoint(c.OgmiosURL))
	kupoClient := kugo.New(kugo.WithEndpoint(c.KupoURL))
	chainContext := OgmiosChainContext.NewOgmiosChainContext(ogmiosClient, kupoClient)
	chainContext.BaseContext = ctx
	chainContext.RequestTimeout = c.requestTimeout()
	if err := chainContext.Init(); err != nil {
		return nil, topup.WrapError(topup.CodeChainUnavailable, "initialize Apollo Ogmios chain context", err)
	}

	return &chainContext, nil
}

func (c Client) sourceUTxOs(ctx context.Context, address string) ([]apolloUTxO.UTxO, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()

	ogmiosClient := ogmigo.New(ogmigo.WithEndpoint(c.OgmiosURL))
	utxos, err := ogmiosClient.UtxosByAddress(requestCtx, address)
	if err != nil {
		return nil, topup.WrapError(topup.CodeChainUnavailable, "query source UTxOs from Ogmios", err)
	}

	results := make([]apolloUTxO.UTxO, 0, len(utxos))
	for _, utxo := range utxos {
		apolloUTxOValue, err := OgmiosChainContext.Utxo_OgmigoToApollo(utxo)
		if err != nil {
			return nil, topup.WrapError(topup.CodeChainUnavailable, "convert Ogmios UTxO to Apollo UTxO", err)
		}
		results = append(results, apolloUTxOValue)
	}

	return results, nil
}

func (c Client) submit(
	ctx context.Context,
	chainContext *OgmiosChainContext.OgmiosChainContext,
	request topup.ChainRequest,
	sourceUTxOs []apolloUTxO.UTxO,
) (string, []string, error) {
	vkey, err := validateRawKeyHex(request.Source.Name, "verification", request.Source.VerificationKeyHex)
	if err != nil {
		return "", nil, err
	}
	skey, err := validateRawKeyHex(request.Source.Name, "signing", request.Source.SigningKeyHex)
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
		return "", nil, topup.WrapError(topup.CodeChainUnavailable, "set faucet source as change address", err)
	}

	slot, err := chainContext.LastBlockSlot()
	if err != nil {
		return "", nil, topup.WrapError(topup.CodeChainUnavailable, "read latest block slot", err)
	}
	builder.
		SetValidityStart(int64(slot)).
		SetTtl(int64(slot)+c.ttlSlots()).
		PayToAddressBech32(request.DestinationAddress, lovelace)

	builder, _, err = builder.Complete()
	if err != nil {
		return "", nil, topup.WrapError(topup.CodeChainUnavailable, "complete top-up transaction", err)
	}
	if err := validateTransaction(
		builder.GetTx(),
		request.DestinationAddress,
		request.Source.Address,
		request.Lovelace,
		sourceUTxOs,
		c.maxFeeLovelace(),
	); err != nil {
		return "", nil, err
	}
	builder.Sign()
	tx := builder.GetTx()
	spentKeys := spentInputKeys(tx.TransactionBody.Inputs)
	if len(spentKeys) == 0 {
		return "", nil, topup.Errorf(topup.CodeChainUnavailable, "submit top-up transaction spent no source inputs")
	}

	txID, err := c.submitSignedTransaction(ctx, tx)
	if err != nil {
		return "", nil, err
	}

	return txID, spentKeys, nil
}

func validateRequest(request topup.ChainRequest) error {
	if strings.TrimSpace(request.Source.Name) == "" {
		return topup.Errorf(topup.CodeInvalidRequest, "submit top-up with Apollo: source name is required")
	}
	if strings.TrimSpace(request.DestinationAddress) == "" {
		return topup.Errorf(topup.CodeInvalidRequest, "submit top-up with Apollo: destination address is required")
	}
	if err := sources.ValidateTestnetAddress(request.DestinationAddress); err != nil {
		return topup.WrapError(topup.CodeInvalidRequest, "submit top-up with Apollo: invalid destination address", err)
	}
	if request.Lovelace <= 0 {
		return topup.Errorf(topup.CodeInvalidRequest, "submit top-up with Apollo: lovelace must be positive")
	}
	if err := sources.ValidateFundingSource(request.Source); err != nil {
		return topup.WrapError(topup.CodeInvalidRequest, "submit top-up with Apollo: invalid source", err)
	}
	if request.DestinationAddress == request.Source.Address {
		return topup.Errorf(topup.CodeInvalidRequest, "submit top-up with Apollo: destination address must not equal source address")
	}

	return nil
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
	tx *apolloTx.Transaction,
	destinationAddress string,
	sourceAddress string,
	lovelace int64,
	sourceUTxOs []apolloUTxO.UTxO,
	maxFeeLovelace int64,
) error {
	if tx == nil {
		return topup.Errorf(topup.CodeChainUnavailable, "complete top-up transaction returned nil transaction")
	}
	if tx.TransactionBody.Fee < 0 {
		return topup.Errorf(topup.CodeChainUnavailable, "complete top-up transaction created a negative fee")
	}
	if tx.TransactionBody.Fee > maxFeeLovelace {
		return topup.Errorf(
			topup.CodeChainUnavailable,
			"complete top-up transaction fee %d exceeds maximum %d",
			tx.TransactionBody.Fee,
			maxFeeLovelace,
		)
	}
	if lovelace > math.MaxInt64-tx.TransactionBody.Fee {
		return topup.Errorf(topup.CodeChainUnavailable, "complete top-up transaction lovelace and fee overflow")
	}

	sourceByInput := make(map[string]apolloUTxO.UTxO, len(sourceUTxOs))
	for _, sourceUTxO := range sourceUTxOs {
		if sourceUTxO.Output.GetAddress().String() != sourceAddress {
			return topup.Errorf(
				topup.CodeChainUnavailable,
				"source UTxO %s has unexpected address %s",
				sourceUTxO.GetKey(),
				sourceUTxO.Output.GetAddress().String(),
			)
		}
		sourceByInput[strings.ToLower(sourceUTxO.GetKey())] = sourceUTxO
	}
	if len(tx.TransactionBody.Inputs) == 0 {
		return topup.Errorf(topup.CodeChainUnavailable, "complete top-up transaction spent no source inputs")
	}

	matches := 0
	sourceInputLovelace := int64(0)
	sourceChangeLovelace := int64(0)
	for _, input := range tx.TransactionBody.Inputs {
		sourceUTxO, ok := sourceByInput[strings.ToLower(inputKey(input))]
		if !ok {
			return topup.Errorf(
				topup.CodeChainUnavailable,
				"complete top-up transaction spent non-source input %s",
				inputKey(input),
			)
		}
		sourceInputLovelace += sourceUTxO.Output.Lovelace()
	}
	for _, output := range tx.TransactionBody.Outputs {
		outputAddress := output.GetAddress().String()
		if outputAddress == sourceAddress {
			sourceChangeLovelace += output.Lovelace()
			continue
		}
		if outputAddress != destinationAddress {
			return topup.Errorf(
				topup.CodeChainUnavailable,
				"complete top-up transaction created an unexpected output to %s",
				outputAddress,
			)
		}

		matches++
		if output.Lovelace() != lovelace {
			return topup.Errorf(
				topup.CodeChainUnavailable,
				"complete top-up transaction changed destination lovelace from %d to %d",
				lovelace,
				output.Lovelace(),
			)
		}
		if !output.GetValue().GetAssets().RemoveZeroAssets().IsEmpty() {
			return topup.Errorf(topup.CodeChainUnavailable, "complete top-up transaction added assets to destination output")
		}
	}
	if matches != 1 {
		return topup.Errorf(
			topup.CodeChainUnavailable,
			"complete top-up transaction created %d destination outputs, want 1",
			matches,
		)
	}
	sourceLoss := sourceInputLovelace - sourceChangeLovelace
	expectedLoss := lovelace + tx.TransactionBody.Fee
	if sourceLoss != expectedLoss {
		return topup.Errorf(
			topup.CodeChainUnavailable,
			"complete top-up transaction consumed %d source lovelace, want %d",
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

func (c Client) submitSignedTransaction(ctx context.Context, tx *apolloTx.Transaction) (string, error) {
	if tx == nil {
		return "", topup.Errorf(topup.CodeChainUnavailable, "submit top-up transaction: transaction is nil")
	}

	txBytes, err := tx.Bytes()
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "encode top-up transaction", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()

	response, err := c.ogmiosSubmitter().SubmitTx(requestCtx, hex.EncodeToString(txBytes))
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "submit top-up transaction to Ogmios", err)
	}
	if response == nil {
		return "", topup.Errorf(topup.CodeChainUnavailable, "submit top-up transaction to Ogmios returned no response")
	}
	if response.Error != nil {
		return "", topup.Errorf(
			topup.CodeChainUnavailable,
			"submit top-up transaction rejected by Ogmios: code %d: %s",
			response.Error.Code,
			strings.TrimSpace(response.Error.Message),
		)
	}

	txID, err := tx.TransactionBody.Id()
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "compute submitted transaction id", err)
	}
	txIDHex := hex.EncodeToString(txID.Payload)
	if responseID := strings.ToLower(strings.TrimSpace(response.ID)); responseID != "" && responseID != txIDHex {
		return "", topup.Errorf(
			topup.CodeChainUnavailable,
			"submit top-up transaction to Ogmios returned transaction id %q, want %q",
			response.ID,
			txIDHex,
		)
	}

	return txIDHex, nil
}

func (c Client) ogmiosSubmitter() ogmiosSubmitter {
	if c.submitter != nil {
		return c.submitter
	}

	return ogmigo.New(ogmigo.WithEndpoint(c.OgmiosURL))
}

func sourceKeyAddress(source sources.FundingSource) (string, error) {
	vkey, err := validateRawKeyHex(source.Name, "verification", source.VerificationKeyHex)
	if err != nil {
		return "", err
	}
	skey, err := validateRawKeyHex(source.Name, "signing", source.SigningKeyHex)
	if err != nil {
		return "", err
	}

	builder := apolloapi.New(apolloapi.NewEmptyBackend()).
		SetWalletFromKeypair(vkey, skey, constants.TESTNET)
	apolloWallet := builder.GetWallet()
	if apolloWallet == nil || apolloWallet.GetAddress() == nil {
		return "", topup.Errorf(
			topup.CodeChainUnavailable,
			"derive source %q address: Apollo wallet was not set",
			source.Name,
		)
	}

	return apolloWallet.GetAddress().String(), nil
}

func validateRawKeyHex(name string, kind string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return "", topup.WrapError(
			topup.CodeInvalidRequest,
			fmt.Sprintf("decode source %q %s key hex", name, kind),
			err,
		)
	}
	if len(decoded) != rawKeyLength {
		return "", topup.Errorf(
			topup.CodeInvalidRequest,
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
		return 0, topup.WrapError(
			topup.CodeInvalidRequest,
			fmt.Sprintf("lovelace %d cannot be represented as int", value),
			err,
		)
	}

	return amount, nil
}

func (c Client) requestTimeout() time.Duration {
	if c.RequestTimeout <= 0 {
		return defaultRequestTimeout
	}

	return c.RequestTimeout
}

func (c Client) ttlSlots() int64 {
	if c.TTLSlots <= 0 {
		return defaultTTLSlots
	}

	return c.TTLSlots
}

func (c Client) maxFeeLovelace() int64 {
	if c.MaxFeeLovelace <= 0 {
		return defaultMaxFeeLovelace
	}

	return c.MaxFeeLovelace
}
