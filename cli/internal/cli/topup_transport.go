package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// faucetAuthTokenKey is the Secret data key under which the faucet auth
// Bearer token lives.
const faucetAuthTokenKey = "token"

// topUpHTTPPayload is the request body sent to the faucet's /v1/topups
// endpoint. Source is optional; an empty value tells the faucet to pick
// from its configured defaults.
type topUpHTTPPayload struct {
	Address  string `json:"address"`
	Lovelace int64  `json:"lovelace"`
	Source   string `json:"source,omitempty"`
}

// topUpHTTPResult is the JSON envelope the faucet returns on a successful
// top-up. The CLI prints these fields directly in both text and JSON modes.
type topUpHTTPResult struct {
	TxID               string `json:"txId"`
	Source             string `json:"source"`
	SourceAddress      string `json:"sourceAddress"`
	DestinationAddress string `json:"destinationAddress"`
	Lovelace           int64  `json:"lovelace"`
}

// faucetErrorResponse is the JSON envelope the faucet uses for typed
// failures. Both fields are best-effort; if either is empty the CLI falls
// back to a generic "HTTP <status>" message.
type faucetErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// postTopUp issues the authenticated top-up request to the faucet and
// decodes the response. A non-2xx response is translated through
// decodeFaucetError into a typed message; a successful response with an
// empty transaction id is treated as a faucet-side bug.
func postTopUp(ctx context.Context, client HTTPDoer, faucetURL string, token string, payload topUpHTTPPayload) (topUpHTTPResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(token) == "" {
		return topUpHTTPResult{}, fmt.Errorf("faucet auth token is empty")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return topUpHTTPResult{}, fmt.Errorf("marshal top-up request: %w", err)
	}
	endpoint := strings.TrimRight(faucetURL, "/") + "/v1/topups"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return topUpHTTPResult{}, fmt.Errorf("build top-up request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return topUpHTTPResult{}, fmt.Errorf("submit top-up request: %w", err)
	}
	defer func() {
		// Drain and close so the underlying transport can reuse the connection.
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return topUpHTTPResult{}, decodeFaucetError(response)
	}

	var result topUpHTTPResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return topUpHTTPResult{}, fmt.Errorf("decode top-up response: %w", err)
	}
	if strings.TrimSpace(result.TxID) == "" {
		return topUpHTTPResult{}, fmt.Errorf("faucet returned an empty transaction id")
	}

	return result, nil
}

// decodeFaucetError turns a non-2xx faucet response into a typed error
// message. The body is read with a 16 KiB cap so a misbehaving faucet
// cannot exhaust memory on the failure path.
func decodeFaucetError(response *http.Response) error {
	var body faucetErrorResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&body); err == nil {
		code := strings.TrimSpace(body.Error.Code)
		message := strings.TrimSpace(body.Error.Message)
		if code != "" && message != "" {
			return fmt.Errorf("faucet top-up failed: HTTP %d: %s: %s", response.StatusCode, code, message)
		}
	}

	return fmt.Errorf("faucet top-up failed: HTTP %d", response.StatusCode)
}
