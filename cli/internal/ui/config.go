package ui

// ColorMode is the requested color policy before TTY and NO_COLOR folding.
type ColorMode int

const (
	// ColorAuto enables color only when the human stream is a terminal.
	ColorAuto ColorMode = iota
	// ColorAlways forces color on regardless of TTY (NO_COLOR still wins).
	ColorAlways
	// ColorNever forces color off.
	ColorNever
)

// RuntimeView is the command layer's already-validated runtime payload in the
// shape ui needs. The command package adapts its RuntimeConfig into this view
// so ui never imports the command package, which would form an import cycle and
// pull charm-banned dependencies into ui's graph.
type RuntimeView struct {
	OutputJSON bool   // OutputFormat == "json"
	Quiet      bool   // -q
	Verbosity  int    // raw -v count
	LogLevel   string // resolved base level after the -v raise
	LogFormat  string // text | json
}

// Config is the resolved UX runtime, computed once and final.
type Config struct {
	OutputJSON     bool
	Quiet          bool
	NonInteractive bool // effective one-way latch
	Color          bool // final on/off after auto|always|never + NO_COLOR + TTY
	Verbosity      int
	LogLevel       string
	LogFormat      string
}

// ConfigFromRuntime derives a final Config from the runtime view plus the
// already-resolved color and non-interactive verdicts. It is the single
// derivation path; the command layer computes color and nonInteractive from the
// raw injected writers, NO_COLOR, and the interactivity latch before calling it.
func ConfigFromRuntime(view RuntimeView, color, nonInteractive bool) Config {
	return Config{
		OutputJSON:     view.OutputJSON,
		Quiet:          view.Quiet,
		NonInteractive: nonInteractive,
		Color:          color,
		Verbosity:      view.Verbosity,
		LogLevel:       view.LogLevel,
		LogFormat:      view.LogFormat,
	}
}
