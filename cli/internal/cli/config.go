package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// RuntimeConfig is the resolved persistent-flag payload. Each field is the
// trimmed, validated value of the corresponding root flag, with the viper
// precedence (flag > env > default) already applied.
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

	// LogLevel is one of debug, info, warn, error.
	LogLevel string

	// LogFormat is text or json.
	LogFormat string
}

// initializeConfig wires viper to the root persistent flags. It is called
// from PersistentPreRunE so flag-vs-environment precedence is established
// once per execution before any RunE.
func initializeConfig(cmd *cobra.Command, vp *viper.Viper) error {
	vp.SetDefault("log-level", "info")
	vp.SetDefault("log-format", "text")
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

// loadRuntimeConfig reads the trimmed runtime values from viper and
// validates the log-level/format enums. The empty-string defaults are
// applied here so callers can compare against the package-known set
// directly.
func loadRuntimeConfig(vp *viper.Viper) (RuntimeConfig, error) {
	config := RuntimeConfig{
		Kubeconfig:  strings.TrimSpace(vp.GetString("kubeconfig")),
		KubeContext: strings.TrimSpace(vp.GetString("kube-context")),
		Namespace:   strings.TrimSpace(vp.GetString("namespace")),
		LogLevel:    strings.TrimSpace(vp.GetString("log-level")),
		LogFormat:   strings.TrimSpace(vp.GetString("log-format")),
	}
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.LogFormat == "" {
		config.LogFormat = "text"
	}

	switch config.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return RuntimeConfig{}, fmt.Errorf("unsupported log level %q", config.LogLevel)
	}
	switch config.LogFormat {
	case "text", "json":
	default:
		return RuntimeConfig{}, fmt.Errorf("unsupported log format %q", config.LogFormat)
	}

	return config, nil
}

// newLogger constructs the slog logger for the active command from the
// resolved RuntimeConfig. JSON format is selected explicitly; everything
// else falls back to text.
func newLogger(config RuntimeConfig, out io.Writer) *slog.Logger {
	var level slog.Level
	switch config.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handlerOptions := &slog.HandlerOptions{Level: level}
	if config.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(out, handlerOptions))
	}

	return slog.New(slog.NewTextHandler(out, handlerOptions))
}
