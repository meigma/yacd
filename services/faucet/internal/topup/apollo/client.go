package apollo

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	apolloapi "github.com/Salvionied/apollo"
	"github.com/Salvionied/apollo/constants"
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

	txID, err := c.submit(chainContext, request, sourceUTxOs)
	if err != nil {
		return topup.ChainResult{}, err
	}

	return topup.ChainResult{TxID: txID}, nil
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
	chainContext *OgmiosChainContext.OgmiosChainContext,
	request topup.ChainRequest,
	sourceUTxOs []apolloUTxO.UTxO,
) (string, error) {
	vkey, err := validateRawKeyHex(request.Source.Name, "verification", request.Source.VerificationKeyHex)
	if err != nil {
		return "", err
	}
	skey, err := validateRawKeyHex(request.Source.Name, "signing", request.Source.SigningKeyHex)
	if err != nil {
		return "", err
	}
	lovelace, err := intLovelace(request.Lovelace)
	if err != nil {
		return "", err
	}

	builder := apolloapi.New(chainContext).SetWalletFromKeypair(vkey, skey, constants.TESTNET)
	builder = builder.AddLoadedUTxOs(sourceUTxOs...)
	builder, err = builder.SetWalletAsChangeAddress()
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "set faucet source as change address", err)
	}

	slot, err := chainContext.LastBlockSlot()
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "read latest block slot", err)
	}
	builder.
		SetValidityStart(int64(slot)).
		SetTtl(int64(slot)+c.ttlSlots()).
		PayToAddressBech32(request.DestinationAddress, lovelace)

	builder, _, err = builder.Complete()
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "complete top-up transaction", err)
	}
	builder.Sign()
	txID, err := builder.Submit()
	if err != nil {
		return "", topup.WrapError(topup.CodeChainUnavailable, "submit top-up transaction", err)
	}

	return hex.EncodeToString(txID.Payload), nil
}

func validateRequest(request topup.ChainRequest) error {
	if strings.TrimSpace(request.Source.Name) == "" {
		return topup.Errorf(topup.CodeInvalidRequest, "submit top-up with Apollo: source name is required")
	}
	if strings.TrimSpace(request.Source.Address) == "" {
		return topup.Errorf(topup.CodeInvalidRequest, "submit top-up with Apollo: source address is required")
	}
	if err := sources.ValidateTestnetAddress(request.Source.Address); err != nil {
		return topup.WrapError(topup.CodeInvalidRequest, "submit top-up with Apollo: invalid source address", err)
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
	if _, err := validateRawKeyHex(request.Source.Name, "verification", request.Source.VerificationKeyHex); err != nil {
		return err
	}
	if _, err := validateRawKeyHex(request.Source.Name, "signing", request.Source.SigningKeyHex); err != nil {
		return err
	}

	return nil
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
