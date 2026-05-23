package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/meigma/yacd/services/faucet/internal/topup"
)

const (
	testDefaultSource = "utxo1"
	testAuthToken     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testInputKey      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb:0"
)

var (
	testSigningRawKeyHex       = strings.Repeat("01", 32)
	testVerificationRawKeyHex  = deriveTestVerificationKeyHex(testSigningRawKeyHex)
	testVerificationKeyCBORHex = "5820" + testVerificationRawKeyHex
	testSigningKeyCBORHex      = "5820" + testSigningRawKeyHex
	testSourceAddress          = mustDeriveTestnetPaymentAddress(testVerificationRawKeyHex)
	testDestinationAddress     = mustDeriveTestnetPaymentAddress(deriveTestVerificationKeyHex(strings.Repeat("02", 32)))
)

func TestHandlerHealth(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodGet, "/healthz")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body statusResponse
	decodeResponse(t, response, &body)
	if body.Status != "ok" {
		t.Fatalf("status body = %q, want ok", body.Status)
	}
}

func TestHandlerReady(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodGet, "/readyz")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestHandlerReadyReportsMissingDefault(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, "utxo9"), http.MethodGet, "/readyz")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != codeNotReady {
		t.Fatalf("error code = %q, want %q", body.Error.Code, codeNotReady)
	}
}

func TestHandlerListsSources(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodGet, "/v1/sources")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body sources.List
	decodeResponse(t, response, &body)
	if body.DefaultSource != testDefaultSource {
		t.Fatalf("defaultSource = %q, want utxo1", body.DefaultSource)
	}
	if len(body.Sources) != 2 {
		t.Fatalf("sources length = %d, want 2", len(body.Sources))
	}
	if body.Sources[0].Name != testDefaultSource || !body.Sources[0].Default {
		t.Fatalf("first source = %#v, want default utxo1", body.Sources[0])
	}
}

func TestHandlerReturnsOneSource(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodGet, "/v1/sources/utxo2")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body sources.Source
	decodeResponse(t, response, &body)
	if body.Name != "utxo2" {
		t.Fatalf("source name = %q, want utxo2", body.Name)
	}
}

func TestHandlerReturnsSourceNotFound(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodGet, "/v1/sources/utxo9")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != sources.CodeSourceNotFound {
		t.Fatalf("error code = %q, want %q", body.Error.Code, sources.CodeSourceNotFound)
	}
}

func TestHandlerSubmitsTopUp(t *testing.T) {
	t.Parallel()

	submitter := &fakeSubmitter{result: topup.ChainResult{TxID: "abc123", SpentInputKeys: []string{testInputKey}}}
	response := performTopUpRequest(
		t,
		testHandlerWithSubmitter(t, testDefaultSource, submitter),
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var body topup.Result
	decodeResponse(t, response, &body)
	if body.TxID != "abc123" {
		t.Fatalf("txId = %q, want abc123", body.TxID)
	}
	if body.Source != testDefaultSource {
		t.Fatalf("source = %q, want utxo1", body.Source)
	}
	if body.DestinationAddress != testDestinationAddress {
		t.Fatalf("destinationAddress = %q, want %q", body.DestinationAddress, testDestinationAddress)
	}
	if len(submitter.requests) != 1 {
		t.Fatalf("submitter requests = %d, want 1", len(submitter.requests))
	}
	if submitter.requests[0].Lovelace != 1_000_000 {
		t.Fatalf("submitted lovelace = %d, want 1000000", submitter.requests[0].Lovelace)
	}
}

func TestHandlerRejectsMalformedTopUpJSON(t *testing.T) {
	t.Parallel()

	response := performTopUpRequest(
		t,
		testHandler(t, testDefaultSource),
		`{"address":`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != topup.CodeInvalidRequest {
		t.Fatalf("error code = %q, want %q", body.Error.Code, topup.CodeInvalidRequest)
	}
}

func TestHandlerRejectsUnknownTopUpFields(t *testing.T) {
	t.Parallel()

	response := performTopUpRequest(
		t,
		testHandler(t, testDefaultSource),
		`{"address":"`+testDestinationAddress+`","lovelace":1,"extra":true}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestHandlerRejectsTopUpUnsupportedMethod(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodGet, "/v1/topups")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got, want := response.Header().Get("Allow"), http.MethodPost; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestHandlerTopUpRequiresBearerAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing"},
		{name: "wrong scheme", authorization: "Basic " + testAuthToken},
		{name: "wrong token", authorization: "Bearer wrong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response := performRawRequestBody(
				t,
				testHandler(t, testDefaultSource),
				http.MethodPost,
				"/v1/topups",
				`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
				map[string]string{
					"Authorization": tt.authorization,
					"Content-Type":  "application/json",
				},
			)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if got, want := response.Header().Get("WWW-Authenticate"), "Bearer"; got != want {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
			}
			var body errorResponse
			decodeResponse(t, response, &body)
			if body.Error.Code != codeUnauthorized {
				t.Fatalf("error code = %q, want %q", body.Error.Code, codeUnauthorized)
			}
		})
	}
}

func TestHandlerTopUpRequiresJSONContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
	}{
		{name: "missing"},
		{name: "text plain", contentType: "text/plain"},
		{name: "malformed", contentType: "application/json; charset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response := performRawRequestBody(
				t,
				testHandler(t, testDefaultSource),
				http.MethodPost,
				"/v1/topups",
				`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
				map[string]string{
					"Authorization": "Bearer " + testAuthToken,
					"Content-Type":  tt.contentType,
				},
			)

			if response.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
			}
			var body errorResponse
			decodeResponse(t, response, &body)
			if body.Error.Code != codeUnsupportedMedia {
				t.Fatalf("error code = %q, want %q", body.Error.Code, codeUnsupportedMedia)
			}
		})
	}
}

func TestHandlerTopUpAllowsJSONContentTypeParameters(t *testing.T) {
	t.Parallel()

	response := performRawRequestBody(
		t,
		testHandler(t, testDefaultSource),
		http.MethodPost,
		"/v1/topups",
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
		map[string]string{
			"Authorization": "Bearer " + testAuthToken,
			"Content-Type":  "application/json; charset=utf-8",
		},
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestHandlerTopUpReportsSourceNotFound(t *testing.T) {
	t.Parallel()

	response := performTopUpRequest(
		t,
		testHandler(t, testDefaultSource),
		`{"address":"`+testDestinationAddress+`","lovelace":1000000,"source":"utxo9"}`,
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != topup.CodeSourceNotFound {
		t.Fatalf("error code = %q, want %q", body.Error.Code, topup.CodeSourceNotFound)
	}
}

func TestHandlerTopUpReportsSourceUnavailable(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, testDefaultSource)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.addr"), testSourceAddress)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.vkey"), `{"type":"bad","cborHex":"`+testVerificationKeyCBORHex+`"}`)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.skey"), `{"type":"GenesisUTxOSigningKey_ed25519","cborHex":"`+testSigningKeyCBORHex+`"}`)
	store := sources.NewStore(rootDir, testDefaultSource)
	handler := NewHandler(
		store,
		topup.NewService(store, &fakeSubmitter{}, topup.Config{MaxLovelace: 10_000_000}),
		testAuthToken,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)

	response := performTopUpRequest(
		t,
		handler,
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != topup.CodeSourceUnavailable {
		t.Fatalf("error code = %q, want %q", body.Error.Code, topup.CodeSourceUnavailable)
	}
}

func TestHandlerTopUpReportsChainFailure(t *testing.T) {
	t.Parallel()

	response := performTopUpRequest(
		t,
		testHandlerWithSubmitter(t, testDefaultSource, &fakeSubmitter{err: errors.New("chain failed")}),
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != topup.CodeChainUnavailable {
		t.Fatalf("error code = %q, want %q", body.Error.Code, topup.CodeChainUnavailable)
	}
}

func TestHandlerTopUpReloadsAuthTokenFile(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, testDefaultSource)
	store := sources.NewStore(rootDir, testDefaultSource)
	currentToken := testAuthToken
	handler := NewHandlerWithAuthTokenFile(
		store,
		topup.NewService(store, &fakeSubmitter{
			result: topup.ChainResult{TxID: "abc123", SpentInputKeys: []string{testInputKey}},
		}, topup.Config{MaxLovelace: 10_000_000}),
		"/token",
		func(string) (string, error) {
			return currentToken, nil
		},
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)

	response := performTopUpRequest(
		t,
		handler,
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	currentToken = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	response = performTopUpRequest(
		t,
		handler,
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status with old token = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	response = performRawRequestBody(
		t,
		handler,
		http.MethodPost,
		"/v1/topups",
		`{"address":"`+testDestinationAddress+`","lovelace":1000000}`,
		map[string]string{
			"Authorization": "Bearer " + currentToken,
			"Content-Type":  "application/json",
		},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status with new token = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestHandlerRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, testDefaultSource), http.MethodPost, "/v1/sources")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got, want := response.Header().Get("Allow"), http.MethodGet; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != codeMethodNotAllowed {
		t.Fatalf("error code = %q, want %q", body.Error.Code, codeMethodNotAllowed)
	}
}

func testHandler(t *testing.T, defaultSource string) http.Handler {
	t.Helper()

	return testHandlerWithSubmitter(t, defaultSource, &fakeSubmitter{
		result: topup.ChainResult{TxID: "abc123", SpentInputKeys: []string{testInputKey}},
	})
}

func testHandlerWithSubmitter(t *testing.T, defaultSource string, submitter topup.TransactionSubmitter) http.Handler {
	t.Helper()

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo2")
	writeSource(t, rootDir, testDefaultSource)
	store := sources.NewStore(rootDir, defaultSource)

	return NewHandler(
		store,
		topup.NewService(store, submitter, topup.Config{MaxLovelace: 10_000_000}),
		testAuthToken,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)
}

func performRequest(t *testing.T, handler http.Handler, method string, path string) *httptest.ResponseRecorder {
	t.Helper()

	return performRequestBody(t, handler, method, path, "")
}

func performRequestBody(t *testing.T, handler http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	return performRawRequestBody(t, handler, method, path, body, nil)
}

func performTopUpRequest(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	return performRawRequestBody(
		t,
		handler,
		http.MethodPost,
		"/v1/topups",
		body,
		map[string]string{
			"Authorization": "Bearer " + testAuthToken,
			"Content-Type":  "application/json",
		},
	)
}

func performRawRequestBody(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for key, value := range headers {
		if value == "" {
			continue
		}
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v\n%s", err, response.Body.String())
	}
}

func writeSource(t *testing.T, rootDir string, name string) {
	t.Helper()

	sourceDir := filepath.Join(rootDir, name)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.addr"), testSourceAddress)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.vkey"), `{
  "type": "GenesisUTxOVerificationKey_ed25519",
  "description": "Genesis Initial UTxO Verification Key",
  "cborHex": "`+testVerificationKeyCBORHex+`"
}`)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.skey"), `{
  "type": "GenesisUTxOSigningKey_ed25519",
  "description": "Genesis Initial UTxO Signing Key",
  "cborHex": "`+testSigningKeyCBORHex+`"
}`)
}

func writeSourceFile(t *testing.T, path string, contents string) {
	t.Helper()

	requireNoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func requireNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

type fakeSubmitter struct {
	result   topup.ChainResult
	err      error
	requests []topup.ChainRequest
}

func (f *fakeSubmitter) SubmitTopUp(_ context.Context, request topup.ChainRequest) (topup.ChainResult, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return topup.ChainResult{}, f.err
	}
	if len(f.result.SpentInputKeys) == 0 {
		f.result.SpentInputKeys = []string{testInputKey}
	}

	return f.result, nil
}
