package apply

import "fmt"

// UnsupportedError represents a known unsupported controller apply condition
// that should be surfaced through status rather than returned as an unexpected
// reconcile error.
type UnsupportedError struct {
	Reason  string
	Message string
}

func (e UnsupportedError) Error() string {
	return e.Message
}

// Unsupported returns an UnsupportedError with a stable reason and formatted
// user-facing message.
func Unsupported(reason string, format string, args ...any) UnsupportedError {
	return UnsupportedError{
		Reason:  reason,
		Message: fmt.Sprintf(format, args...),
	}
}
