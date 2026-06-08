package ui

import (
	"encoding/json"
	"fmt"
	"io"
)

// Data returns the data plane (stdout). It is never styled; text-mode renderers
// write here directly, while structured payloads go through Encode.
func (i IO) Data() io.Writer { return i.out }

// Encode writes value to the data plane as json.MarshalIndent(value, "", "  ")
// followed by a newline, byte-identical to the CLI's existing list/info/wallet
// JSON paths. MarshalIndent applies SetEscapeHTML(true), so the frozen output
// shapes need no re-pinning. The bytes never depend on TTY, color, or verbosity.
func (i IO) Encode(value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if _, err := fmt.Fprintf(i.out, "%s\n", encoded); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}
