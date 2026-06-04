package tx

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	// CodeInvalidRequest identifies caller input that cannot be submitted.
	CodeInvalidRequest = "invalid_request"
	// CodeChainUnavailable identifies a chain client or submission failure.
	CodeChainUnavailable = "chain_unavailable"
)

// Submitter builds, validates, signs, and submits one funding transaction.
type Submitter interface {
	// Submit submits one transaction for the requested source and amount.
	Submit(ctx context.Context, request Request) (Result, error)
}

// Request describes one funding transaction in chain primitives.
//
// The source key material is the raw 32-byte ed25519 payment key pair encoded
// as hex, not a cardano-cli envelope. Callers that hold keys behind a store or
// Secret are responsible for decoding their material into these fields.
type Request struct {
	// SourceName labels the source in errors and results. It is not used to
	// locate key material; the caller supplies the keys directly.
	SourceName string
	// SourceAddress is the Cardano testnet address that funds the transaction.
	SourceAddress string
	// VerificationKeyHex is the source raw 32-byte verification key as hex.
	VerificationKeyHex string
	// SigningKeyHex is the source raw 32-byte signing key as hex.
	SigningKeyHex string
	// DestinationAddress is the Cardano testnet recipient address.
	DestinationAddress string
	// Lovelace is the exact amount to pay the destination.
	Lovelace int64
	// ExcludeInputKeys are source UTxO input keys already pending from earlier
	// submissions and must not be reused.
	ExcludeInputKeys []string
}

// Result describes a submitted funding transaction.
type Result struct {
	// TxID is the submitted transaction id as lowercase hex.
	TxID string
	// SpentInputKeys are source UTxO input keys consumed by the transaction.
	SpentInputKeys []string
}

// Error is a structured transaction-engine error.
type Error struct {
	// Code is a stable machine-readable error code.
	Code string
	// Message is a human-readable error message.
	Message string
	// Cause is the wrapped lower-level error, when one exists.
	Cause error
}

func (e *Error) Error() string {
	return e.Message
}

// Unwrap returns the lower-level error that caused e.
func (e *Error) Unwrap() error {
	return e.Cause
}

// IsCode reports whether err is an engine Error with code.
func IsCode(err error, code string) bool {
	var txErr *Error
	ok := errors.As(err, &txErr)

	return ok && txErr.Code == code
}

// Errorf creates a structured transaction-engine error.
func Errorf(code string, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// WrapError creates a structured transaction-engine error with a lower-level
// cause.
func WrapError(code string, message string, cause error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.SourceName) == "" {
		return Errorf(CodeInvalidRequest, "submit funding transaction: source name is required")
	}
	if strings.TrimSpace(request.DestinationAddress) == "" {
		return Errorf(CodeInvalidRequest, "submit funding transaction: destination address is required")
	}
	if err := ValidateTestnetAddress(request.DestinationAddress); err != nil {
		return WrapError(CodeInvalidRequest, "submit funding transaction: invalid destination address", err)
	}
	if request.Lovelace <= 0 {
		return Errorf(CodeInvalidRequest, "submit funding transaction: lovelace must be positive")
	}
	if err := validateSourceKeys(request); err != nil {
		return WrapError(CodeInvalidRequest, "submit funding transaction: invalid source", err)
	}
	if request.DestinationAddress == request.SourceAddress {
		return Errorf(CodeInvalidRequest, "submit funding transaction: destination address must not equal source address")
	}

	return nil
}
