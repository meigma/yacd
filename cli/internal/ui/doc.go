// Package ui is the terminal IO seam for the YACD developer CLI. It owns the
// split between the data plane (stdout, never styled) and the human plane
// (stderr, where diagnostics, progress, warnings, and prompts live), so the
// boundary is enforced by one value type instead of per-call-site discipline.
//
// IO is the single surface, built once by New from a resolved Config and the
// injected streams and threaded by value on the command context. Data and
// Encode are the only writers to stdout; Encode reproduces the CLI's frozen
// MarshalIndent JSON byte-for-byte. Info, Status, Success, Detail, Warn, and
// Error write the human plane, gated by Quiet so only Error survives the mute.
// Config is derived once through ConfigFromRuntime from a RuntimeView the
// command layer supplies, keeping this package free of any dependency on the
// command layer and on charm.
//
// IsTerminal is the one TTY definition for the whole CLI. Reporter selects a
// plain or no-op lifecycle.Reporter for long-running work; the animated
// reporter and lipgloss styling arrive in a later phase, so this interim
// realization depends only on the standard library, golang.org/x/term, and
// cli/internal/lifecycle.
package ui
