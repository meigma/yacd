package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/meigma/yacd/services/faucet/internal/server"
	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultListenAddress = "127.0.0.1:8080"
	defaultUTXOKeysDir   = "/state/env/utxo-keys"
	defaultSource        = "utxo1"
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

	ServerRunner func(*server.Config) error
}

type commandContext struct {
	err          io.Writer
	viper        *viper.Viper
	serverRunner func(*server.Config) error
	logger       *slog.Logger
}

// RuntimeConfig is the faucet process runtime configuration.
type RuntimeConfig struct {
	ListenAddress string
	UTXOKeysDir   string
	DefaultSource string
	LogLevel      string
	LogFormat     string
}

// NewRootCommand creates the YACD faucet service command.
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
	if options.ServerRunner == nil {
		options.ServerRunner = func(config *server.Config) error {
			return server.Run(config)
		}
	}
	options.Build = options.Build.withDefaults()

	commandContext := &commandContext{
		err:          options.Err,
		viper:        options.Viper,
		serverRunner: options.ServerRunner,
		logger: slog.New(
			slog.NewTextHandler(options.Err, &slog.HandlerOptions{Level: slog.LevelInfo}),
		),
	}

	root := &cobra.Command{
		Use:           "yacd-faucet",
		Short:         "YACD faucet service",
		Version:       options.Build.Version,
		Args:          cobra.NoArgs,
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}

			return commandContext.serverRunner(&server.Config{
				Context:       cmd.Context(),
				ListenAddress: runtimeConfig.ListenAddress,
				Sources: sources.NewStore(
					runtimeConfig.UTXOKeysDir,
					runtimeConfig.DefaultSource,
				),
				Logger: commandContext.logger,
			})
		},
	}
	root.SetVersionTemplate(
		fmt.Sprintf(
			"yacd-faucet %s (%s) built %s\n",
			options.Build.Version,
			options.Build.Commit,
			options.Build.Date,
		),
	)
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)

	root.Flags().String(
		"listen-address",
		defaultListenAddress,
		"Address for the HTTP server to listen on; set 0.0.0.0:8080 to expose beyond loopback",
	)
	root.Flags().String("utxo-keys-dir", defaultUTXOKeysDir, "Path to the cardano-testnet utxo-keys directory")
	root.Flags().String("default-source", defaultSource, "Default faucet source name")
	root.Flags().String("log-level", "info", "Log level: debug, info, warn, error")
	root.Flags().String("log-format", "text", "Log format: text, json")

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
	vp.SetDefault("listen-address", defaultListenAddress)
	vp.SetDefault("utxo-keys-dir", defaultUTXOKeysDir)
	vp.SetDefault("default-source", defaultSource)
	vp.SetDefault("log-level", "info")
	vp.SetDefault("log-format", "text")
	vp.SetEnvPrefix("YACD_FAUCET")
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	rootFlags := cmd.Root().Flags()
	for _, flag := range []struct {
		key  string
		name string
	}{
		{key: "listen-address", name: "listen-address"},
		{key: "utxo-keys-dir", name: "utxo-keys-dir"},
		{key: "default-source", name: "default-source"},
		{key: "log-level", name: "log-level"},
		{key: "log-format", name: "log-format"},
	} {
		if err := bindFlag(vp, flag.key, rootFlags.Lookup(flag.name)); err != nil {
			return err
		}
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
		ListenAddress: strings.TrimSpace(vp.GetString("listen-address")),
		UTXOKeysDir:   strings.TrimSpace(vp.GetString("utxo-keys-dir")),
		DefaultSource: strings.TrimSpace(vp.GetString("default-source")),
		LogLevel:      strings.TrimSpace(vp.GetString("log-level")),
		LogFormat:     strings.TrimSpace(vp.GetString("log-format")),
	}
	if config.ListenAddress == "" {
		return RuntimeConfig{}, fmt.Errorf("--listen-address is required")
	}
	if config.UTXOKeysDir == "" {
		return RuntimeConfig{}, fmt.Errorf("--utxo-keys-dir is required")
	}
	if err := sources.ValidateName(config.DefaultSource); err != nil {
		return RuntimeConfig{}, fmt.Errorf("invalid --default-source: %w", err)
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
