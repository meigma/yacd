package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/meigma/yacd/services/faucet/internal/topup"
)

const (
	codeMethodNotAllowed = "method_not_allowed"
	codeNotFound         = "not_found"
	codeNotReady         = "not_ready"
	codeInternalError    = "internal_error"
	maxTopUpBodyBytes    = 4 * 1024
)

// Config describes the faucet HTTP server runtime configuration.
type Config struct {
	Context       context.Context
	ListenAddress string
	Sources       sources.Store
	TopUps        topup.Service
	Logger        *slog.Logger
}

type handler struct {
	sources sources.Store
	topups  topup.Service
	logger  *slog.Logger
}

type statusResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error responseError `json:"error"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type topUpRequest struct {
	Address  string `json:"address"`
	Lovelace *int64 `json:"lovelace"`
	Source   string `json:"source,omitempty"`
}

// NewHandler builds the faucet HTTP handler.
func NewHandler(store sources.Store, topups topup.Service, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return &handler{
		sources: store,
		topups:  topups,
		logger:  logger,
	}
}

// Run starts the faucet HTTP server and gracefully shuts it down when the
// configured context is canceled.
func Run(config *Config) error {
	if config.Context == nil {
		config.Context = context.Background()
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	httpServer := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           NewHandler(config.Sources, config.TopUps, config.Logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			errCh <- nil
			return
		}
		errCh <- err
	}()

	select {
	case <-config.Context.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown faucet server: %w", err)
		}
		if err := <-errCh; err != nil {
			return fmt.Errorf("serve faucet: %w", err)
		}

		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("serve faucet: %w", err)
		}

		return nil
	}
}

func (h *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.URL.Path == "/healthz":
		h.handleHealth(writer, request)
	case request.URL.Path == "/readyz":
		h.handleReady(writer, request)
	case request.URL.Path == "/v1/sources":
		h.handleSourceList(writer, request)
	case request.URL.Path == "/v1/topups":
		h.handleTopUp(writer, request)
	case strings.HasPrefix(request.URL.Path, "/v1/sources/"):
		h.handleSource(writer, request)
	default:
		writeError(writer, http.StatusNotFound, codeNotFound, "route was not found")
	}
}

func (h *handler) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if !requireGet(writer, request) {
		return
	}

	writeJSON(writer, http.StatusOK, statusResponse{Status: "ok"})
}

func (h *handler) handleReady(writer http.ResponseWriter, request *http.Request) {
	if !requireGet(writer, request) {
		return
	}

	if err := h.sources.Ready(); err != nil {
		writeError(writer, http.StatusServiceUnavailable, codeNotReady, err.Error())
		return
	}

	writeJSON(writer, http.StatusOK, statusResponse{Status: "ok"})
}

func (h *handler) handleSourceList(writer http.ResponseWriter, request *http.Request) {
	if !requireGet(writer, request) {
		return
	}

	list, err := h.sources.List()
	if err != nil {
		h.writeSourceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, list)
}

func (h *handler) handleSource(writer http.ResponseWriter, request *http.Request) {
	if !requireGet(writer, request) {
		return
	}

	name, err := url.PathUnescape(strings.TrimPrefix(request.URL.Path, "/v1/sources/"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, sources.CodeInvalidSourceName, "source name is invalid")
		return
	}

	source, err := h.sources.Get(name)
	if err != nil {
		h.writeSourceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, source)
}

func (h *handler) handleTopUp(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}

	var body topUpRequest
	if err := decodeRequestBody(writer, request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, topup.CodeInvalidRequest, err.Error())
		return
	}
	if body.Lovelace == nil {
		writeError(writer, http.StatusBadRequest, topup.CodeInvalidRequest, "lovelace is required")
		return
	}

	result, err := h.topups.Submit(request.Context(), topup.Request{
		Source:             body.Source,
		DestinationAddress: body.Address,
		Lovelace:           *body.Lovelace,
	})
	if err != nil {
		h.writeTopUpError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, result)
}

func (h *handler) writeSourceError(writer http.ResponseWriter, err error) {
	var sourceErr *sources.Error
	if !errors.As(err, &sourceErr) {
		h.logger.Error("Unhandled source error", "error", err)
		writeError(writer, http.StatusInternalServerError, codeInternalError, "source error")
		return
	}

	switch sourceErr.Code {
	case sources.CodeInvalidSourceName:
		writeError(writer, http.StatusBadRequest, sourceErr.Code, sourceErr.Message)
	case sources.CodeSourceNotFound:
		writeError(writer, http.StatusNotFound, sourceErr.Code, sourceErr.Message)
	case sources.CodeSourceIncomplete:
		writeError(writer, http.StatusNotFound, sources.CodeSourceNotFound, sourceErr.Message)
	default:
		writeError(writer, http.StatusInternalServerError, sourceErr.Code, sourceErr.Message)
	}
}

func (h *handler) writeTopUpError(writer http.ResponseWriter, err error) {
	var topUpErr *topup.Error
	if !errors.As(err, &topUpErr) {
		h.logger.Error("Unhandled top-up error", "error", err)
		writeError(writer, http.StatusInternalServerError, codeInternalError, "top-up error")
		return
	}

	switch topUpErr.Code {
	case topup.CodeInvalidRequest:
		writeError(writer, http.StatusBadRequest, topUpErr.Code, topUpErr.Message)
	case topup.CodeSourceNotFound:
		writeError(writer, http.StatusNotFound, topUpErr.Code, topUpErr.Message)
	case topup.CodeSourceUnavailable, topup.CodeChainUnavailable:
		writeError(writer, http.StatusServiceUnavailable, topUpErr.Code, topUpErr.Message)
	default:
		h.logger.Error("Unhandled structured top-up error", "code", topUpErr.Code, "error", err)
		writeError(writer, http.StatusInternalServerError, codeInternalError, topUpErr.Message)
	}
}

func requireGet(writer http.ResponseWriter, request *http.Request) bool {
	return requireMethod(writer, request, http.MethodGet)
}

func requireMethod(writer http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}

	writer.Header().Set("Allow", method)
	writeError(
		writer,
		http.StatusMethodNotAllowed,
		codeMethodNotAllowed,
		fmt.Sprintf("method %s is not allowed", request.Method),
	)

	return false
}

func decodeRequestBody(writer http.ResponseWriter, request *http.Request, target any) error {
	reader := http.MaxBytesReader(writer, request.Body, maxTopUpBodyBytes)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode JSON request body: multiple JSON values are not allowed")
	}

	return nil
}

func writeJSON(writer http.ResponseWriter, statusCode int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(body); err != nil {
		slog.Default().Error("Failed to write JSON response", "error", err)
	}
}

func writeError(writer http.ResponseWriter, statusCode int, code string, message string) {
	writeJSON(writer, statusCode, errorResponse{
		Error: responseError{
			Code:    code,
			Message: message,
		},
	})
}
