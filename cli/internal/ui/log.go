package ui

import "log/slog"

// NewSlogLogger builds the command logger on the human plane (stderr). Under
// Quiet it returns a discard logger regardless of level — the global mute forces
// logging off, overriding -v and --log-level — while the final returned error
// still prints through the exit handler, which is not the logger. Otherwise it
// returns a stdlib text or JSON handler at the given level. The styling phase
// swaps the text path to a colored handler behind this unchanged signature; the
// JSON path stays stdlib for log-shape stability.
func (i IO) NewSlogLogger(level slog.Level, jsonFormat bool) *slog.Logger {
	if i.cfg.Quiet {
		return slog.New(slog.DiscardHandler)
	}
	options := &slog.HandlerOptions{Level: level}
	if jsonFormat {
		return slog.New(slog.NewJSONHandler(i.errw, options))
	}
	return slog.New(slog.NewTextHandler(i.errw, options))
}
