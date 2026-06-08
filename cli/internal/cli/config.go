package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/meigma/yacd/cli/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// formatText and formatJSON are the shared values for the --log-format and
// --output enums.
const (
	formatText = "text"
	formatJSON = "json"
)

// RuntimeConfig is the resolved persistent-flag payload, computed once in the
// root PersistentPreRunE and cached on commandContext. Precedence-bearing
// fields come from viper (flag > env > default); the session/TTY knobs come
// straight off cmd.Flags() so no ambient YACD_* can drive them.
type RuntimeConfig struct {
	// Kubeconfig is the optional kubeconfig path; empty defers to standard
	// loading rules.
	Kubeconfig string

	// KubeContext is the optional kubeconfig context; empty defers to
	// current-context.
	KubeContext string

	// Namespace is the override namespace for the active command; empty
	// defers to environment or kubeconfig defaults.
	Namespace string

	// LogLevel is one of debug, info, warn, error, after the -v raise.
	LogLevel string

	// LogFormat is text or json.
	LogFormat string

	// OutputFormat is the data-plane format: text or json. It is the sole
	// machine-output toggle (the per-command --json flags are removed).
	OutputFormat string

	// Verbosity is the raw -v count.
	Verbosity int

	// Quiet is -q: the global mute (human plane, progress, warnings, logger).
	Quiet bool

	// NonInteractive disables prompts (the requested half of the latch).
	NonInteractive bool

	// Color is the requested color policy before NO_COLOR and TTY folding.
	Color ui.ColorMode
}

// initializeConfig wires viper to the root persistent flags. It is called
// from PersistentPreRunE so flag-vs-environment precedence is established
// once per execution before any RunE.
func initializeConfig(cmd *cobra.Command, vp *viper.Viper) error {
	vp.SetDefault("log-level", "info")
	vp.SetDefault("log-format", formatText)
	vp.SetEnvPrefix("YACD")
	// Environment variable names cannot contain "-" or ".", so the replacer
	// canonicalises both to "_". Flag and viper-key names already use "-".
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	rootFlags := cmd.Root().PersistentFlags()
	if err := bindFlag(vp, "kubeconfig", rootFlags.Lookup("kubeconfig")); err != nil {
		return err
	}
	if err := bindFlag(vp, "kube-context", rootFlags.Lookup("context")); err != nil {
		return err
	}
	if err := bindFlag(vp, "namespace", rootFlags.Lookup("namespace")); err != nil {
		return err
	}
	if err := bindFlag(vp, "log-level", rootFlags.Lookup("log-level")); err != nil {
		return err
	}
	if err := bindFlag(vp, "log-format", rootFlags.Lookup("log-format")); err != nil {
		return err
	}
	if err := bindFlag(vp, "output", rootFlags.Lookup("output")); err != nil {
		return err
	}
	if err := vp.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("bind command flags: %w", err)
	}

	return nil
}

// bindFlag wraps viper.BindPFlag with a guard against a missing flag, so
// renaming a persistent flag without updating initializeConfig fails fast
// instead of silently dropping the binding.
func bindFlag(vp *viper.Viper, key string, flag *pflag.Flag) error {
	if flag == nil {
		return fmt.Errorf("bind flag %q: flag is missing", key)
	}
	if err := vp.BindPFlag(key, flag); err != nil {
		return fmt.Errorf("bind flag %q: %w", key, err)
	}

	return nil
}

// loadRuntimeConfig resolves the full runtime payload once. The precedence
// values are read through viper; the flag-only session knobs (-v/-q/
// --non-interactive/--color/--no-color) are read straight off cmd.Flags() so an
// ambient YACD_* cannot drive them or leak into a child environment. -v is
// additive over the resolved --log-level. All enums are validated here.
func loadRuntimeConfig(cmd *cobra.Command, vp *viper.Viper) (RuntimeConfig, error) {
	config := RuntimeConfig{
		Kubeconfig:   strings.TrimSpace(vp.GetString("kubeconfig")),
		KubeContext:  strings.TrimSpace(vp.GetString("kube-context")),
		Namespace:    strings.TrimSpace(vp.GetString("namespace")),
		LogLevel:     strings.TrimSpace(vp.GetString("log-level")),
		LogFormat:    strings.TrimSpace(vp.GetString("log-format")),
		OutputFormat: strings.TrimSpace(vp.GetString("output")),
	}
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.LogFormat == "" {
		config.LogFormat = formatText
	}
	if config.OutputFormat == "" {
		config.OutputFormat = formatText
	}

	flags := cmd.Flags()
	verbosity, err := flags.GetCount("verbose")
	if err != nil {
		return RuntimeConfig{}, err
	}
	quiet, err := flags.GetBool("quiet")
	if err != nil {
		return RuntimeConfig{}, err
	}
	nonInteractive, err := flags.GetBool("non-interactive")
	if err != nil {
		return RuntimeConfig{}, err
	}
	noColor, err := flags.GetBool("no-color")
	if err != nil {
		return RuntimeConfig{}, err
	}
	colorValue, err := flags.GetString("color")
	if err != nil {
		return RuntimeConfig{}, err
	}

	config.Verbosity = verbosity
	config.Quiet = quiet
	config.NonInteractive = nonInteractive

	switch config.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return RuntimeConfig{}, fmt.Errorf("unsupported log level %q", config.LogLevel)
	}
	config.LogLevel = raise(config.LogLevel, verbosity)

	switch config.LogFormat {
	case formatText, formatJSON:
	default:
		return RuntimeConfig{}, fmt.Errorf("unsupported log format %q", config.LogFormat)
	}
	switch config.OutputFormat {
	case formatText, formatJSON:
	default:
		return RuntimeConfig{}, fmt.Errorf("unsupported output format %q", config.OutputFormat)
	}

	color, err := resolveColorMode(colorValue, noColor, flags.Changed("color"))
	if err != nil {
		return RuntimeConfig{}, err
	}
	config.Color = color

	return config, nil
}

// resolveColorMode folds --color and --no-color into a single requested policy.
// An explicit --color wins over --no-color; otherwise --no-color forces never.
// NO_COLOR is applied later, in resolveUX, where it is supreme.
func resolveColorMode(value string, noColor, colorChanged bool) (ui.ColorMode, error) {
	if noColor && !colorChanged {
		return ui.ColorNever, nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return ui.ColorAuto, nil
	case "always":
		return ui.ColorAlways, nil
	case "never":
		return ui.ColorNever, nil
	default:
		return ui.ColorAuto, fmt.Errorf("unsupported color mode %q", value)
	}
}

// resolveUX turns the requested color policy and interactivity into final
// verdicts from the raw injected streams. NO_COLOR is supreme over
// --color=always; the non-interactive latch is one-way (any non-TTY stream or
// the flag disables prompts).
func resolveUX(config RuntimeConfig, errOut io.Writer, in io.Reader) (color, nonInteractive bool) {
	errTTY := ui.IsTerminal(errOut)
	inTTY := ui.IsTerminalReader(in)

	switch {
	case os.Getenv("NO_COLOR") != "":
		color = false
	case config.Color == ui.ColorAlways:
		color = true
	case config.Color == ui.ColorNever:
		color = false
	default: // ColorAuto
		color = errTTY
	}

	nonInteractive = config.NonInteractive || !errTTY || !inTTY
	return color, nonInteractive
}

// view adapts the command-layer RuntimeConfig into the ui package's input
// shape, keeping ui free of any dependency on this package.
func (config RuntimeConfig) view() ui.RuntimeView {
	return ui.RuntimeView{
		OutputJSON: config.OutputFormat == formatJSON,
		Quiet:      config.Quiet,
		Verbosity:  config.Verbosity,
		LogLevel:   config.LogLevel,
		LogFormat:  config.LogFormat,
	}
}

// raise lifts the base log level by verbose steps along the ordering
// error < warn < info < debug. It never lowers the level and caps at debug; a
// base outside the known set (already rejected by loadRuntimeConfig) is
// returned unchanged. This is the one two-key composition: -v is additive over
// the resolved --log-level.
func raise(base string, verbose int) string {
	if verbose <= 0 {
		return base
	}
	order := []string{"error", "warn", "info", "debug"}
	index := -1
	for i, level := range order {
		if level == base {
			index = i
			break
		}
	}
	if index < 0 {
		return base
	}
	if raised := index + verbose; raised < len(order) {
		return order[raised]
	}
	return order[len(order)-1]
}

// slogLevel maps the resolved log-level string to a slog.Level. Unknown values
// (already rejected by loadRuntimeConfig) fall back to info.
func slogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
