package topup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/meigma/yacd/services/faucet/internal/sources"
)

const (
	// DefaultMaxLovelace is the default upper bound for a single top-up request.
	DefaultMaxLovelace int64 = 10_000_000_000

	// CodeInvalidRequest identifies caller input that cannot be submitted.
	CodeInvalidRequest = "invalid_request"
	// CodeSourceNotFound identifies a missing faucet source.
	CodeSourceNotFound = sources.CodeSourceNotFound
	// CodeSourceUnavailable identifies a source that exists but cannot be used.
	CodeSourceUnavailable = "source_unavailable"
	// CodeChainUnavailable identifies a chain client or submission failure.
	CodeChainUnavailable = "chain_unavailable"
)

// SourceReader loads private faucet source material for top-up submission.
type SourceReader interface {
	// DefaultName returns the configured default source name.
	DefaultName() string
	// ReadFundingSource returns the selected source with private key material.
	ReadFundingSource(ctx context.Context, name string) (sources.FundingSource, error)
}

// TransactionSubmitter submits one exact top-up transaction.
type TransactionSubmitter interface {
	// SubmitTopUp submits one transaction for the requested source and amount.
	SubmitTopUp(ctx context.Context, request ChainRequest) (ChainResult, error)
}

// Service coordinates faucet top-up requests.
type Service struct {
	sourceReader SourceReader
	submitter    TransactionSubmitter
	maxLovelace  int64
	locks        *sourceLocks
}

type sourceLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Request describes one exact top-up submission request.
type Request struct {
	// Source is the optional source name. Empty selects the configured default.
	Source string
	// DestinationAddress is the Cardano testnet recipient address.
	DestinationAddress string
	// Lovelace is the exact amount to submit.
	Lovelace int64
}

// Result describes a submitted top-up transaction.
type Result struct {
	// TxID is the submitted transaction id as lowercase hex.
	TxID string `json:"txId"`
	// Source is the faucet source name used for the transaction.
	Source string `json:"source"`
	// SourceAddress is the faucet source payment address.
	SourceAddress string `json:"sourceAddress"`
	// DestinationAddress is the requested recipient address.
	DestinationAddress string `json:"destinationAddress"`
	// Lovelace is the exact submitted amount.
	Lovelace int64 `json:"lovelace"`
}

// ChainRequest is the transaction-level request passed to the chain submitter.
type ChainRequest struct {
	// Source is the private faucet source used to sign the transaction.
	Source sources.FundingSource
	// DestinationAddress is the Cardano testnet recipient address.
	DestinationAddress string
	// Lovelace is the exact amount to submit.
	Lovelace int64
}

// ChainResult is the transaction-level result returned by the chain submitter.
type ChainResult struct {
	// TxID is the submitted transaction id as lowercase hex.
	TxID string
}

// Error is a structured top-up error.
type Error struct {
	// Code is a stable machine-readable error code.
	Code string
	// Message is a human-readable error message.
	Message string
	// Cause is the wrapped lower-level error, when one exists.
	Cause error
}

// NewService constructs a top-up service from source and transaction dependencies.
func NewService(sourceReader SourceReader, submitter TransactionSubmitter, maxLovelace int64) Service {
	return Service{
		sourceReader: sourceReader,
		submitter:    submitter,
		maxLovelace:  maxLovelace,
		locks:        newSourceLocks(),
	}
}

// Submit submits one exact faucet top-up transaction.
func (s Service) Submit(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, WrapError(CodeChainUnavailable, "submit top-up: context canceled", err)
	}
	if s.sourceReader == nil {
		return Result{}, Errorf(CodeSourceUnavailable, "submit top-up: source reader is not configured")
	}
	if s.submitter == nil {
		return Result{}, Errorf(CodeChainUnavailable, "submit top-up: transaction submitter is not configured")
	}

	sourceName := strings.TrimSpace(request.Source)
	if sourceName == "" {
		sourceName = s.sourceReader.DefaultName()
	}
	if err := validateRequest(sourceName, request, s.limit()); err != nil {
		return Result{}, err
	}

	source, err := s.sourceReader.ReadFundingSource(ctx, sourceName)
	if err != nil {
		return Result{}, mapSourceError(sourceName, err)
	}

	unlock := s.lockSource(source.Name)
	defer unlock()

	chainResult, err := s.submitter.SubmitTopUp(ctx, ChainRequest{
		Source:             source,
		DestinationAddress: request.DestinationAddress,
		Lovelace:           request.Lovelace,
	})
	if err != nil {
		var topupErr *Error
		if errors.As(err, &topupErr) {
			return Result{}, topupErr
		}

		return Result{}, WrapError(CodeChainUnavailable, "submit top-up transaction", err)
	}
	if strings.TrimSpace(chainResult.TxID) == "" {
		return Result{}, Errorf(CodeChainUnavailable, "submit top-up transaction returned an empty transaction id")
	}

	return Result{
		TxID:               strings.ToLower(strings.TrimSpace(chainResult.TxID)),
		Source:             source.Name,
		SourceAddress:      source.Address,
		DestinationAddress: request.DestinationAddress,
		Lovelace:           request.Lovelace,
	}, nil
}

func validateRequest(sourceName string, request Request, maxLovelace int64) error {
	if err := sources.ValidateName(sourceName); err != nil {
		return WrapError(CodeInvalidRequest, "invalid source name", err)
	}
	if err := sources.ValidateTestnetAddress(request.DestinationAddress); err != nil {
		return WrapError(CodeInvalidRequest, "invalid destination address", err)
	}
	if request.Lovelace <= 0 {
		return Errorf(CodeInvalidRequest, "lovelace must be positive")
	}
	if request.Lovelace > maxLovelace {
		return Errorf(CodeInvalidRequest, "lovelace must be at most %d", maxLovelace)
	}

	return nil
}

func mapSourceError(sourceName string, err error) error {
	switch {
	case sources.IsCode(err, sources.CodeSourceNotFound), sources.IsCode(err, sources.CodeSourceIncomplete):
		return WrapError(CodeSourceNotFound, fmt.Sprintf("faucet source %q was not found", sourceName), err)
	case sources.IsCode(err, sources.CodeInvalidSourceName):
		return WrapError(CodeInvalidRequest, "invalid source name", err)
	default:
		return WrapError(CodeSourceUnavailable, fmt.Sprintf("faucet source %q is unavailable", sourceName), err)
	}
}

func (s Service) limit() int64 {
	if s.maxLovelace <= 0 {
		return DefaultMaxLovelace
	}

	return s.maxLovelace
}

func (s Service) lockSource(sourceName string) func() {
	if s.locks == nil {
		lock := &sync.Mutex{}
		lock.Lock()
		return lock.Unlock
	}

	return s.locks.lock(sourceName)
}

func newSourceLocks() *sourceLocks {
	return &sourceLocks{
		locks: make(map[string]*sync.Mutex),
	}
}

func (s *sourceLocks) lock(sourceName string) func() {
	s.mu.Lock()
	lock, ok := s.locks[sourceName]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[sourceName] = lock
	}
	s.mu.Unlock()

	lock.Lock()

	return lock.Unlock
}

func (e *Error) Error() string {
	return e.Message
}

// Unwrap returns the lower-level error that caused e.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Errorf creates a structured top-up error.
func Errorf(code string, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// WrapError creates a structured top-up error with a lower-level cause.
func WrapError(code string, message string, cause error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
