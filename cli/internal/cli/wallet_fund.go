package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	walletstore "github.com/meigma/yacd/cli/internal/wallet"
	"github.com/meigma/yacd/internal/cardano/tx"
	domainwallet "github.com/meigma/yacd/internal/cardano/wallet"
)

// fundRequest is one resolved funding instruction: the destination address to
// pay, the lovelace amount, the source wallet name to fund from, and whether to
// wait for on-chain confirmation.
type fundRequest struct {
	// destinationAddress is the validated bech32 testnet address to fund.
	destinationAddress string

	// lovelace is the exact amount to pay the destination.
	lovelace int64

	// sourceName is the managed wallet name that signs and funds the transfer.
	sourceName string

	// await reports whether to block until the transfer confirms on-chain.
	await bool

	// awaitTimeout bounds the on-chain confirmation wait when await is set.
	awaitTimeout time.Duration
}

// fundResult is the outcome of a submitted funding transfer.
type fundResult struct {
	// TxID is the submitted transaction id as lowercase hex.
	TxID string `json:"txId"`

	// Source is the wallet name the funds came from.
	Source string `json:"source"`

	// DestinationAddress is the address that was funded.
	DestinationAddress string `json:"destinationAddress"`

	// Lovelace is the amount paid to the destination.
	Lovelace int64 `json:"lovelace"`

	// Confirmed reports whether the transfer was observed on-chain. It is only
	// true when the caller requested --await and confirmation succeeded.
	Confirmed bool `json:"confirmed"`
}

// fundWallet funds destinationAddress from a managed source wallet by building
// and submitting a transaction over self-managed Ogmios/Kupo port-forwards.
//
// The faucet wallet (the default source) signs from its Secret; there is no
// faucet HTTP service in the funding path. fundWallet gates on the network being
// ready (so Ogmios and Kupo are published) and on the source wallet Secret
// existing, reads and decodes the source key pair, forwards the chain APIs to
// loopback, and submits through the injected tx.Submitter so the funding verbs
// are testable without a live chain. When request.await is set it then polls the
// chain index until the destination output appears.
func (commandContext *commandContext) fundWallet(
	ctx context.Context,
	kubeClient kube.Client,
	store *walletstore.Store,
	network *yacdv1alpha1.CardanoNetwork,
	namespace string,
	name string,
	request fundRequest,
) (fundResult, error) {
	if err := requireReady(network, namespace, name); err != nil {
		return fundResult{}, err
	}

	source, err := store.Source(ctx, request.sourceName)
	if err != nil {
		return fundResult{}, err
	}
	if source.Address == request.destinationAddress {
		return fundResult{}, fmt.Errorf("source wallet %q and destination are the same address", source.Name)
	}

	verificationKeyHex, signingKeyHex, err := sourceKeyMaterial(ctx, kubeClient, namespace, source.SecretName)
	if err != nil {
		return fundResult{}, err
	}

	session, endpoints, err := forwardEndpoints(ctx, kubeClient, network, namespace, name)
	if err != nil {
		return fundResult{}, err
	}
	defer func() { _ = session.Close() }()

	if strings.TrimSpace(endpoints.OgmiosURL) == "" || strings.TrimSpace(endpoints.KupoURL) == "" {
		return fundResult{}, fmt.Errorf("cardanonetwork %s/%s does not publish both Ogmios and Kupo endpoints required for funding", namespace, name)
	}

	submitter := commandContext.txSubmitterFactory(endpoints.OgmiosURL, endpoints.KupoURL)
	// Apollo's OgmiosChainContext logs a non-fatal genesis-config fetch warning
	// with a hardcoded fmt.Printf to stdout during chain-context init. Point the
	// process stdout at stderr across submission/confirmation so that third-party
	// noise never corrupts the CLI's stdout (notably --json); the verbs print
	// results through commandContext.out, which keeps the original stdout.
	defer redirectStdoutToStderr()()
	submitResult, err := submitter.Submit(ctx, tx.Request{
		SourceName:         source.Name,
		SourceAddress:      source.Address,
		VerificationKeyHex: verificationKeyHex,
		SigningKeyHex:      signingKeyHex,
		DestinationAddress: request.destinationAddress,
		Lovelace:           request.lovelace,
	})
	if err != nil {
		return fundResult{}, err
	}

	result := fundResult{
		TxID:               submitResult.TxID,
		Source:             source.Name,
		DestinationAddress: request.destinationAddress,
		Lovelace:           request.lovelace,
	}

	if request.await {
		confirmer := commandContext.utxoConfirmerFactory(endpoints.KupoURL)
		// One-time notice to stderr so the otherwise-silent poll does not look
		// hung; it stays off stdout to keep --json output clean and is
		// best-effort because the transfer is already submitted.
		_, _ = fmt.Fprintf(commandContext.err, "Waiting up to %s for %s to confirm on-chain...\n", request.awaitTimeout, result.TxID)
		if err := awaitConfirmation(ctx, confirmer, request.destinationAddress, result.TxID, request.awaitTimeout); err != nil {
			return fundResult{}, err
		}
		result.Confirmed = true
	}

	return result, nil
}

// sourceKeyMaterial reads a wallet Secret and decodes its payment key pair into
// the raw hex the transaction engine expects. The signing and verification key
// envelopes are the cardano-cli text-envelope shape the controller and CLI both
// persist, so they decode through the shared manager-safe envelope decoder.
func sourceKeyMaterial(ctx context.Context, kubeClient kube.Client, namespace string, secretName string) (verificationKeyHex string, signingKeyHex string, err error) {
	secret, err := kubeClient.GetSecret(ctx, namespace, secretName)
	if err != nil {
		return "", "", err
	}

	signingEnvelope, ok := secret.Data[walletstore.SigningKeyKey]
	if !ok || len(signingEnvelope) == 0 {
		return "", "", fmt.Errorf("wallet secret %s/%s is missing %s", namespace, secretName, walletstore.SigningKeyKey)
	}
	verificationEnvelope, ok := secret.Data[walletstore.VerificationKeyKey]
	if !ok || len(verificationEnvelope) == 0 {
		return "", "", fmt.Errorf("wallet secret %s/%s is missing %s", namespace, secretName, walletstore.VerificationKeyKey)
	}

	signingKeyHex, err = domainwallet.DecodePaymentKeyEnvelope(signingEnvelope)
	if err != nil {
		return "", "", fmt.Errorf("decode source signing key: %w", err)
	}
	verificationKeyHex, err = domainwallet.DecodePaymentKeyEnvelope(verificationEnvelope)
	if err != nil {
		return "", "", fmt.Errorf("decode source verification key: %w", err)
	}

	return verificationKeyHex, signingKeyHex, nil
}

// redirectStdoutToStderr points the process stdout at stderr and returns a
// restore func. Some third-party chain libraries print diagnostics with
// fmt.Printf (process stdout) that would otherwise corrupt the CLI's stdout —
// notably --json output. The CLI itself writes through commandContext.out/err,
// which retain the original streams, so its own output is unaffected. It is
// safe because the CLI runs a single command synchronously.
func redirectStdoutToStderr() func() {
	original := os.Stdout
	os.Stdout = os.Stderr

	return func() { os.Stdout = original }
}
