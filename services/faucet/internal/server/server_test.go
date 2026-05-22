package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/services/faucet/internal/sources"
)

func TestHandlerHealth(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, "utxo1"), http.MethodGet, "/healthz")
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

	response := performRequest(t, testHandler(t, "utxo1"), http.MethodGet, "/readyz")
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

	response := performRequest(t, testHandler(t, "utxo1"), http.MethodGet, "/v1/sources")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body sources.List
	decodeResponse(t, response, &body)
	if body.DefaultSource != "utxo1" {
		t.Fatalf("defaultSource = %q, want utxo1", body.DefaultSource)
	}
	if len(body.Sources) != 2 {
		t.Fatalf("sources length = %d, want 2", len(body.Sources))
	}
	if body.Sources[0].Name != "utxo1" || !body.Sources[0].Default {
		t.Fatalf("first source = %#v, want default utxo1", body.Sources[0])
	}
}

func TestHandlerReturnsOneSource(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, "utxo1"), http.MethodGet, "/v1/sources/utxo2")
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

	response := performRequest(t, testHandler(t, "utxo1"), http.MethodGet, "/v1/sources/utxo9")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	var body errorResponse
	decodeResponse(t, response, &body)
	if body.Error.Code != sources.CodeSourceNotFound {
		t.Fatalf("error code = %q, want %q", body.Error.Code, sources.CodeSourceNotFound)
	}
}

func TestHandlerRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	response := performRequest(t, testHandler(t, "utxo1"), http.MethodPost, "/v1/sources")
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

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo2")
	writeSource(t, rootDir, "utxo1")

	return NewHandler(
		sources.NewStore(rootDir, defaultSource),
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)
}

func performRequest(t *testing.T, handler http.Handler, method string, path string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, nil)
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
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.vkey"), `{
  "type": "GenesisUTxOVerificationKey_ed25519",
  "description": "Genesis Initial UTxO Verification Key",
  "cborHex": "public-cbor"
}`)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.skey"), `{
  "type": "GenesisUTxOSigningKey_ed25519",
  "description": "Genesis Initial UTxO Signing Key",
  "cborHex": "secret-cbor"
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
