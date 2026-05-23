package cli

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/meigma/yacd/services/faucet/internal/server"
	"github.com/meigma/yacd/services/faucet/internal/sources"
	"github.com/meigma/yacd/services/faucet/internal/topup"
	topupapollo "github.com/meigma/yacd/services/faucet/internal/topup/apollo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultListenAddress       = "127.0.0.1:8080"
	defaultUTXOKeysDir         = "/state/env/utxo-keys"
	defaultSource              = "utxo1"
	defaultOgmiosURL           = "ws://127.0.0.1:1337"
	defaultKupoURL             = "http://127.0.0.1:1442"
	defaultAuthTokenFile       = "/var/run/yacd-faucet/token" // #nosec G101 -- this is a token file path, not token material.
	defaultChainRequestTimeout = 15 * time.Second
	defaultTxTTLSlots          = 300
	maxAuthTokenBytes          = 8 * 1024
	minAuthTokenLength         = 32
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
	ListenAddress       string
	UTXOKeysDir         string
	DefaultSource       string
	AllowRemoteListen   bool
	OgmiosURL           string
	KupoURL             string
	AuthTokenFile       string
	MinTopUpLovelace    int64
	MaxTopUpLovelace    int64
	ChainRequestTimeout time.Duration
	TxTTLSlots          int64
	LogLevel            string
	LogFormat           string
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
			authToken, err := loadAuthTokenFile(runtimeConfig.AuthTokenFile)
			if err != nil {
				return err
			}

			sourceStore := sources.NewStore(
				runtimeConfig.UTXOKeysDir,
				runtimeConfig.DefaultSource,
			)
			transactionSubmitter := topupapollo.Client{
				OgmiosURL:      runtimeConfig.OgmiosURL,
				KupoURL:        runtimeConfig.KupoURL,
				RequestTimeout: runtimeConfig.ChainRequestTimeout,
				TTLSlots:       runtimeConfig.TxTTLSlots,
			}

			return commandContext.serverRunner(&server.Config{
				Context:       cmd.Context(),
				ListenAddress: runtimeConfig.ListenAddress,
				Sources:       sourceStore,
				TopUps: topup.NewService(
					sourceStore,
					transactionSubmitter,
					topup.Config{
						MinLovelace: runtimeConfig.MinTopUpLovelace,
						MaxLovelace: runtimeConfig.MaxTopUpLovelace,
					},
				),
				AuthToken: authToken,
				Logger:    commandContext.logger,
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
		"Address for the HTTP server to listen on",
	)
	root.Flags().Bool("allow-remote-listen", false, "Allow non-loopback listen addresses for intentional network exposure")
	root.Flags().String("utxo-keys-dir", defaultUTXOKeysDir, "Path to the cardano-testnet utxo-keys directory")
	root.Flags().String("default-source", defaultSource, "Default faucet source name")
	root.Flags().String("ogmios-url", defaultOgmiosURL, "Ogmios websocket URL for transaction submission")
	root.Flags().String("kupo-url", defaultKupoURL, "Kupo HTTP URL for transaction building")
	root.Flags().String("auth-token-file", defaultAuthTokenFile, "Path to a file containing the bearer token required for top-up requests")
	root.Flags().Int64("min-topup-lovelace", topup.DefaultMinLovelace, "Minimum lovelace accepted for one top-up")
	root.Flags().Int64("max-topup-lovelace", topup.DefaultMaxLovelace, "Maximum lovelace accepted for one top-up")
	root.Flags().Duration("chain-request-timeout", defaultChainRequestTimeout, "Timeout for individual chain requests")
	root.Flags().Int64("tx-ttl-slots", defaultTxTTLSlots, "Transaction TTL measured in slots after the latest block")
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
	vp.SetDefault("allow-remote-listen", false)
	vp.SetDefault("utxo-keys-dir", defaultUTXOKeysDir)
	vp.SetDefault("default-source", defaultSource)
	vp.SetDefault("ogmios-url", defaultOgmiosURL)
	vp.SetDefault("kupo-url", defaultKupoURL)
	vp.SetDefault("auth-token-file", defaultAuthTokenFile)
	vp.SetDefault("min-topup-lovelace", topup.DefaultMinLovelace)
	vp.SetDefault("max-topup-lovelace", topup.DefaultMaxLovelace)
	vp.SetDefault("chain-request-timeout", defaultChainRequestTimeout)
	vp.SetDefault("tx-ttl-slots", defaultTxTTLSlots)
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
		{key: "allow-remote-listen", name: "allow-remote-listen"},
		{key: "utxo-keys-dir", name: "utxo-keys-dir"},
		{key: "default-source", name: "default-source"},
		{key: "ogmios-url", name: "ogmios-url"},
		{key: "kupo-url", name: "kupo-url"},
		{key: "auth-token-file", name: "auth-token-file"},
		{key: "min-topup-lovelace", name: "min-topup-lovelace"},
		{key: "max-topup-lovelace", name: "max-topup-lovelace"},
		{key: "chain-request-timeout", name: "chain-request-timeout"},
		{key: "tx-ttl-slots", name: "tx-ttl-slots"},
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
		ListenAddress:       strings.TrimSpace(vp.GetString("listen-address")),
		AllowRemoteListen:   vp.GetBool("allow-remote-listen"),
		UTXOKeysDir:         strings.TrimSpace(vp.GetString("utxo-keys-dir")),
		DefaultSource:       strings.TrimSpace(vp.GetString("default-source")),
		OgmiosURL:           strings.TrimSpace(vp.GetString("ogmios-url")),
		KupoURL:             strings.TrimSpace(vp.GetString("kupo-url")),
		AuthTokenFile:       strings.TrimSpace(vp.GetString("auth-token-file")),
		MinTopUpLovelace:    vp.GetInt64("min-topup-lovelace"),
		MaxTopUpLovelace:    vp.GetInt64("max-topup-lovelace"),
		ChainRequestTimeout: vp.GetDuration("chain-request-timeout"),
		TxTTLSlots:          vp.GetInt64("tx-ttl-slots"),
		LogLevel:            strings.TrimSpace(vp.GetString("log-level")),
		LogFormat:           strings.TrimSpace(vp.GetString("log-format")),
	}
	if config.ListenAddress == "" {
		return RuntimeConfig{}, fmt.Errorf("--listen-address is required")
	}
	if err := validateListenAddress(config.ListenAddress, config.AllowRemoteListen); err != nil {
		return RuntimeConfig{}, err
	}
	if config.UTXOKeysDir == "" {
		return RuntimeConfig{}, fmt.Errorf("--utxo-keys-dir is required")
	}
	if err := sources.ValidateName(config.DefaultSource); err != nil {
		return RuntimeConfig{}, fmt.Errorf("invalid --default-source: %w", err)
	}
	if config.OgmiosURL == "" {
		return RuntimeConfig{}, fmt.Errorf("--ogmios-url is required")
	}
	if config.KupoURL == "" {
		return RuntimeConfig{}, fmt.Errorf("--kupo-url is required")
	}
	if config.AuthTokenFile == "" {
		return RuntimeConfig{}, fmt.Errorf("--auth-token-file is required")
	}
	if config.MinTopUpLovelace <= 0 {
		return RuntimeConfig{}, fmt.Errorf("--min-topup-lovelace must be positive")
	}
	if config.MaxTopUpLovelace <= 0 {
		return RuntimeConfig{}, fmt.Errorf("--max-topup-lovelace must be positive")
	}
	if config.MinTopUpLovelace > config.MaxTopUpLovelace {
		return RuntimeConfig{}, fmt.Errorf("--min-topup-lovelace must be less than or equal to --max-topup-lovelace")
	}
	if config.ChainRequestTimeout <= 0 {
		return RuntimeConfig{}, fmt.Errorf("--chain-request-timeout must be positive")
	}
	if config.TxTTLSlots <= 0 {
		return RuntimeConfig{}, fmt.Errorf("--tx-ttl-slots must be positive")
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

func loadAuthTokenFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("--auth-token-file is required")
	}

	// #nosec G304 -- the token file is an explicit operator-controlled configuration path.
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read --auth-token-file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	contents, err := io.ReadAll(io.LimitReader(file, maxAuthTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("read --auth-token-file: %w", err)
	}
	if len(contents) > maxAuthTokenBytes {
		return "", fmt.Errorf("--auth-token-file is larger than %d bytes", maxAuthTokenBytes)
	}

	token := strings.TrimSpace(string(contents))
	if len(token) < minAuthTokenLength {
		return "", fmt.Errorf("--auth-token-file token must be at least %d characters", minAuthTokenLength)
	}
	for _, character := range token {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", fmt.Errorf("--auth-token-file token must not contain whitespace or control characters")
		}
	}

	return token, nil
}

func validateListenAddress(address string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid --listen-address: %w", err)
	}
	if allowRemote {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}

	return fmt.Errorf("--listen-address %q is not loopback; set --allow-remote-listen for intentional network exposure", address)
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
