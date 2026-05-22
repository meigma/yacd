package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// BuildInfo describes linker-injected build metadata printed by --version.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Options customizes root command construction.
type Options struct {
	In    io.Reader
	Out   io.Writer
	Err   io.Writer
	Build BuildInfo
	Viper *viper.Viper

	KubeClientFactory func(kube.Config) (kube.Client, error)
}

type commandContext struct {
	in                io.Reader
	out               io.Writer
	err               io.Writer
	viper             *viper.Viper
	kubeClientFactory func(kube.Config) (kube.Client, error)
	logger            *slog.Logger
}

// RuntimeConfig is the global CLI runtime configuration.
type RuntimeConfig struct {
	Kubeconfig  string
	KubeContext string
	Namespace   string
	LogLevel    string
	LogFormat   string
}

// NewRootCommand creates the YACD developer CLI command tree.
func NewRootCommand(options Options) *cobra.Command {
	if options.In == nil {
		options.In = strings.NewReader("")
	}
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.Err == nil {
		options.Err = io.Discard
	}
	if options.Viper == nil {
		options.Viper = viper.New()
	}
	if options.KubeClientFactory == nil {
		options.KubeClientFactory = kube.NewClient
	}
	options.Build = options.Build.withDefaults()

	commandContext := &commandContext{
		in:                options.In,
		out:               options.Out,
		err:               options.Err,
		viper:             options.Viper,
		kubeClientFactory: options.KubeClientFactory,
		logger:            slog.New(slog.NewTextHandler(options.Err, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	root := &cobra.Command{
		Use:           "yacd",
		Short:         "YACD developer CLI",
		Version:       options.Build.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := initializeConfig(cmd, commandContext.viper); err != nil {
				return err
			}
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}
			commandContext.logger = newLogger(runtimeConfig, commandContext.err)
			return nil
		},
	}
	root.SetVersionTemplate(fmt.Sprintf("yacd %s (%s) built %s\n", options.Build.Version, options.Build.Commit, options.Build.Date))
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)

	root.PersistentFlags().String("kubeconfig", "", "Path to the kubeconfig file")
	root.PersistentFlags().String("context", "", "Kubeconfig context to use")
	root.PersistentFlags().StringP("namespace", "n", "", "Kubernetes namespace")
	root.PersistentFlags().String("log-level", "info", "Log level: debug, info, warn, error")
	root.PersistentFlags().String("log-format", "text", "Log format: text, json")

	root.AddCommand(newDeployCommand(commandContext))
	root.AddCommand(newInfoCommand(commandContext))

	return root
}

func (b BuildInfo) withDefaults() BuildInfo {
	if strings.TrimSpace(b.Version) == "" {
		b.Version = "dev"
	}
	if strings.TrimSpace(b.Commit) == "" {
		b.Commit = "none"
	}
	if strings.TrimSpace(b.Date) == "" {
		b.Date = "unknown"
	}
	return b
}

func initializeConfig(cmd *cobra.Command, vp *viper.Viper) error {
	vp.SetDefault("log-level", "info")
	vp.SetDefault("log-format", "text")
	vp.SetEnvPrefix("YACD")
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

func bindFlag(vp *viper.Viper, key string, flag *pflag.Flag) error {
	if flag == nil {
		return fmt.Errorf("bind flag %q: flag is missing", key)
	}
	if err := vp.BindPFlag(key, flag); err != nil {
		return fmt.Errorf("bind flag %q: %w", key, err)
	}

	return nil
}

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
