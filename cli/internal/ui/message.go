package ui

import "fmt"

// Info prints a status line to the human plane (stderr). It is a no-op under
// Quiet.
func (i IO) Info(format string, args ...any) { i.human(format, args...) }

// Status prints a progress line to the human plane; long-running commands use
// it. It is a no-op under Quiet.
func (i IO) Status(format string, args ...any) { i.human(format, args...) }

// Success prints a completion line to the human plane. It is a no-op under
// Quiet.
func (i IO) Success(format string, args ...any) { i.human(format, args...) }

// Detail prints an indented detail line to the human plane. It is a no-op under
// Quiet.
func (i IO) Detail(format string, args ...any) {
	if i.cfg.Quiet {
		return
	}
	i.write("    "+format, args...)
}

// Warn prints a warning to the human plane. It is suppressed under Quiet — the
// global mute leaves only errors and data — and prepends "Warning: " so call
// sites carry only the message.
func (i IO) Warn(format string, args ...any) {
	if i.cfg.Quiet {
		return
	}
	i.write("Warning: "+format, args...)
}

// Error prints an error line to the human plane. It is always shown, even under
// Quiet.
func (i IO) Error(format string, args ...any) { i.write(format, args...) }

// human writes a Quiet-gated line to the human plane.
func (i IO) human(format string, args ...any) {
	if i.cfg.Quiet {
		return
	}
	i.write(format, args...)
}

// write emits to the human plane. Callers include their own trailing newline so
// migrated phrases stay byte-identical; this charm-free interim adds no styling.
func (i IO) write(format string, args ...any) {
	_, _ = fmt.Fprintf(i.errw, format, args...)
}
