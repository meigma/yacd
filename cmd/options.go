package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/alecthomas/kong"
	"github.com/go-logr/logr"
)

// managerOptions captures the manager process command-line flags parsed by
// Kong. Defaults and validation tags are sourced from struct tags so that the
// parser, the help output, and the runtime defaults remain in sync.
type managerOptions struct {
	// MetricsBindAddress is the address the metrics endpoint binds to.
	// A value of "0" disables the metrics server entirely.
	MetricsBindAddress string `name:"metrics-bind-address" default:"0" help:"Metrics bind address. Use 0 to disable."`

	// HealthProbeBindAddress is the address the liveness/readiness probe HTTP
	// server binds to.
	HealthProbeBindAddress string `name:"health-probe-bind-address" default:":8081" help:"Health probe bind address."`

	// LeaderElect enables controller-runtime leader election so multiple
	// replicas can run safely without duplicate reconcile work.
	LeaderElect bool `name:"leader-elect" default:"false" help:"Enable leader election."`

	// MetricsSecure controls whether the metrics endpoint serves over HTTPS
	// with Kubernetes authn/authz filtering applied.
	MetricsSecure bool `name:"metrics-secure" default:"true" negatable:"" help:"Serve metrics over HTTPS."`

	// WebhookCertPath is the directory containing the webhook server
	// certificate material; empty disables the certificate watcher.
	WebhookCertPath string `name:"webhook-cert-path" help:"Webhook certificate directory."`

	// WebhookCertName is the filename of the webhook certificate within
	// WebhookCertPath.
	WebhookCertName string `name:"webhook-cert-name" default:"tls.crt" help:"Webhook certificate filename."`

	// WebhookCertKey is the filename of the webhook private key within
	// WebhookCertPath.
	WebhookCertKey string `name:"webhook-cert-key" default:"tls.key" help:"Webhook private key filename."`

	// MetricsCertPath is the directory containing the metrics server
	// certificate material; empty disables the certificate watcher.
	MetricsCertPath string `name:"metrics-cert-path" help:"Metrics certificate directory."`

	// MetricsCertName is the filename of the metrics certificate within
	// MetricsCertPath.
	MetricsCertName string `name:"metrics-cert-name" default:"tls.crt" help:"Metrics certificate filename."`

	// MetricsCertKey is the filename of the metrics private key within
	// MetricsCertPath.
	MetricsCertKey string `name:"metrics-cert-key" default:"tls.key" help:"Metrics private key filename."`

	// EnableHTTP2 opts the metrics and webhook servers back into HTTP/2.
	// HTTP/2 is disabled by default to avoid the rapid-reset CVEs.
	EnableHTTP2 bool `name:"enable-http2" default:"false" help:"Enable HTTP/2 for metrics and webhooks."`

	// LogFormat selects the slog handler used for controller-runtime logs.
	// Allowed values are "json" and "text".
	LogFormat string `name:"log-format" enum:"json,text" default:"json" help:"Log output format."`

	// LogLevel sets the minimum slog level emitted by the controller-runtime
	// logger. Allowed values are "debug", "info", "warn", and "error".
	LogLevel string `name:"log-level" enum:"debug,info,warn,error" default:"info" help:"Minimum log level."`

	// DefaultFaucetImage is the faucet image used when a CardanoNetwork does
	// not provide spec.chainAPI.faucet.image.
	//nolint:lll // Kong option tags are intentionally kept on a single struct field line.
	DefaultFaucetImage string `name:"default-faucet-image" default:"ghcr.io/meigma/yacd/faucet:dev" help:"Default faucet image for CardanoNetwork faucet sidecars."`

	// DefaultCardanoTestnetImage overrides the cardano-testnet container image
	// used for the create-env init container, the faucet source-address init
	// container, and (when spec.node.image is unset) the primary cardano-node
	// container. Empty leaves the built-in
	// "<repo>:<toolVersion>-<revision>" formula in place.
	//nolint:lll // Kong option tags are intentionally kept on a single struct field line.
	DefaultCardanoTestnetImage string `name:"default-cardano-testnet-image" default:"" help:"Override the cardano-testnet image used for init/source-address containers and the default cardano-node container; empty uses the built-in versioned reference."`

	// DefaultCardanoToolsImage overrides the cardano-tools container image used
	// for artifact staging (fetch/generate/serve) in both controllers. Empty
	// leaves the built-in "<repo>:<toolVersion>-<revision>" formula in place.
	//nolint:lll // Kong option tags are intentionally kept on a single struct field line.
	DefaultCardanoToolsImage string `name:"default-cardano-tools-image" default:"" help:"Override the cardano-tools image used for artifact staging containers; empty uses the built-in versioned reference."`
}

// newManagerParser constructs the Kong parser bound to the supplied options
// struct. It centralises the parser name, description, and error behaviour so
// every call site renders the same usage output.
func newManagerParser(options *managerOptions) (*kong.Kong, error) {
	return kong.New(options,
		kong.Name("yacd"),
		kong.Description("Kubernetes controller manager."),
		kong.UsageOnError(),
	)
}

// parseManagerOptions parses the given argument vector into a managerOptions
// value, returning any parser or validation error to the caller.
func parseManagerOptions(args []string) (managerOptions, error) {
	var options managerOptions
	parser, err := newManagerParser(&options)
	if err != nil {
		return managerOptions{}, err
	}

	if _, err := parser.Parse(args); err != nil {
		return managerOptions{}, err
	}

	return options, nil
}

// mustParseManagerOptions parses args into managerOptions and terminates the
// process via Kong's FatalIfErrorf when parsing fails. It is intended for the
// manager entrypoint, where a failed flag parse is unrecoverable.
func mustParseManagerOptions(args []string) managerOptions {
	var options managerOptions
	parser, err := newManagerParser(&options)
	if err != nil {
		panic(err)
	}

	_, err = parser.Parse(args)
	parser.FatalIfErrorf(err)

	return options
}

// slogLevel maps a textual log level name to the corresponding slog.Level.
// Unknown values return slog.LevelInfo together with an error so callers can
// surface the parse failure before the logger is constructed.
func slogLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", level)
	}
}

// mustNewControllerLogger builds the controller-runtime logger and panics if
// the configured log format or level is invalid. It is intended for the
// manager entrypoint after option parsing has already succeeded.
func mustNewControllerLogger(options managerOptions, out io.Writer) logr.Logger {
	logger, err := newControllerLogger(options, out)
	if err != nil {
		panic(err)
	}

	return logger
}

// newControllerLogger builds a logr.Logger backed by a slog handler chosen by
// options.LogFormat and gated by options.LogLevel. It returns an error for
// unknown format or level values so callers can surface configuration
// problems instead of silently falling back to a default.
func newControllerLogger(options managerOptions, out io.Writer) (logr.Logger, error) {
	level, err := slogLevel(options.LogLevel)
	if err != nil {
		return logr.Logger{}, err
	}

	var levelVar slog.LevelVar
	levelVar.Set(level)

	handlerOptions := &slog.HandlerOptions{Level: &levelVar}
	switch options.LogFormat {
	case "json":
		return logr.FromSlogHandler(slog.NewJSONHandler(out, handlerOptions)), nil
	case "text":
		return logr.FromSlogHandler(slog.NewTextHandler(out, handlerOptions)), nil
	default:
		return logr.Logger{}, fmt.Errorf("unsupported log format %q", options.LogFormat)
	}
}
