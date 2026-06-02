package cardanonetwork

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// walletFaucetTopUpPath is the faucet HTTP path that builds and submits a
	// funding transaction.
	walletFaucetTopUpPath = "/v1/topups"
	// walletFundTimeout bounds a single faucet funding request. Building and
	// submitting a transaction is slower than a health probe, so this is more
	// generous than the sync prober's timeout.
	walletFundTimeout = 15 * time.Second
	// walletConfirmTimeout bounds a single Kupo confirmation query.
	walletConfirmTimeout = 5 * time.Second
)

// walletFundingRejectedError marks a funding attempt the faucet definitively
// rejected (an HTTP 4xx), as opposed to a transient connectivity error worth
// retrying. The controller escalates a rejection to Degraded while retrying
// transient errors.
type walletFundingRejectedError struct {
	message string
}

func (e walletFundingRejectedError) Error() string {
	return e.message
}

// walletFunder builds and submits a funding transaction that pays the wallet
// address through the faucet. It is a narrow port so tests can fund without a
// running faucet. Kept HTTP-only (the operator never imports the faucet's
// Apollo/ogmigo transaction client).
type walletFunder interface {
	Fund(ctx context.Context, faucetURL string, token string, address string, lovelace int64) (txID string, err error)
}

// walletConfirmer reports whether the wallet address holds at least the
// requested funding on-chain. It is a narrow port so tests can confirm without
// a running Kupo. Kept HTTP-only (the operator never imports the kugo client,
// which would drag the ogmigo/Gorilla-WebSocket stack into the manager).
type walletConfirmer interface {
	Confirmed(ctx context.Context, kupoURL string, address string, minLovelace int64) (bool, error)
}

// walletFunderFunc adapts a function to [walletFunder] for tests.
type walletFunderFunc func(ctx context.Context, faucetURL string, token string, address string, lovelace int64) (string, error)

// Fund implements [walletFunder].
func (f walletFunderFunc) Fund(ctx context.Context, faucetURL string, token string, address string, lovelace int64) (string, error) {
	return f(ctx, faucetURL, token, address, lovelace)
}

// walletConfirmerFunc adapts a function to [walletConfirmer] for tests.
type walletConfirmerFunc func(ctx context.Context, kupoURL string, address string, minLovelace int64) (bool, error)

// Confirmed implements [walletConfirmer].
func (c walletConfirmerFunc) Confirmed(ctx context.Context, kupoURL string, address string, minLovelace int64) (bool, error) {
	return c(ctx, kupoURL, address, minLovelace)
}

// walletFunder returns the configured faucet funder.
func (r *CardanoNetworkReconciler) walletFunder() walletFunder {
	if r.walletFunderOverride != nil {
		return r.walletFunderOverride
	}

	return defaultWalletFunder{httpClient: http.DefaultClient}
}

// walletConfirmer returns the configured Kupo confirmer.
func (r *CardanoNetworkReconciler) walletConfirmer() walletConfirmer {
	if r.walletConfirmerOverride != nil {
		return r.walletConfirmerOverride
	}

	return defaultWalletConfirmer{httpClient: http.DefaultClient}
}

// defaultWalletFunder posts a top-up request to the in-cluster faucet.
type defaultWalletFunder struct {
	httpClient *http.Client
}

// faucetTopUpRequest mirrors the faucet's POST /v1/topups payload. Source is
// omitted so the faucet spends from its configured default source.
type faucetTopUpRequest struct {
	Address  string `json:"address"`
	Lovelace int64  `json:"lovelace"`
}

// faucetTopUpResponse is the subset of the faucet response the controller uses.
type faucetTopUpResponse struct {
	TxID string `json:"txId"`
}

// Fund implements [walletFunder] by posting to the faucet's top-up endpoint
// with a bearer token.
func (f defaultWalletFunder) Fund(ctx context.Context, faucetURL string, token string, address string, lovelace int64) (string, error) {
	endpoint, err := url.JoinPath(faucetURL, walletFaucetTopUpPath)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(faucetTopUpRequest{Address: address, Lovelace: lovelace})
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	client := f.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		message := fmt.Sprintf("faucet returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
		// A 4xx is a definitive rejection of the request (not a transient
		// connectivity error), so the controller should stop retrying and
		// surface it; 5xx and connection errors stay retryable.
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			return "", walletFundingRejectedError{message: message}
		}
		return "", fmt.Errorf("%s", message)
	}

	var decoded faucetTopUpResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("decode faucet response: %w", err)
	}
	if strings.TrimSpace(decoded.TxID) == "" {
		return "", fmt.Errorf("faucet response did not include a transaction id")
	}

	return decoded.TxID, nil
}

// defaultWalletConfirmer queries Kupo's REST API for unspent outputs at the
// wallet address. It deliberately speaks Kupo's HTTP API directly instead of
// importing the kugo client.
type defaultWalletConfirmer struct {
	httpClient *http.Client
}

// kupoMatch is the subset of a Kupo match the controller needs to total the
// lovelace held at an address.
type kupoMatch struct {
	Value struct {
		Coins int64 `json:"coins"`
	} `json:"value"`
}

// Confirmed implements [walletConfirmer] by summing the unspent lovelace Kupo
// reports for the address and comparing it to the requested funding.
func (c defaultWalletConfirmer) Confirmed(ctx context.Context, kupoURL string, address string, minLovelace int64) (bool, error) {
	endpoint, err := url.JoinPath(kupoURL, "matches", address)
	if err != nil {
		return false, err
	}
	endpoint += "?unspent"

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("kupo returned HTTP %d", response.StatusCode)
	}

	var matches []kupoMatch
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&matches); err != nil {
		return false, fmt.Errorf("decode kupo matches: %w", err)
	}

	var total int64
	for _, match := range matches {
		total += match.Value.Coins
	}

	return total >= minLovelace, nil
}
