package cli

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/viper"
)

// BuildInfo carries the linker-injected version metadata that --version
// prints. GoReleaser sets all three fields with ldflags at release time;
// development builds default to "dev"/"none"/"unknown".
type BuildInfo struct {
	// Version is the semver of the release, or "dev" for development builds.
	Version string

	// Commit is the short git SHA the binary was built from, or "none".
	Commit string

	// Date is the build timestamp in RFC3339, or "unknown".
	Date string
}

// withDefaults returns a copy of BuildInfo with empty fields filled in with
// the development-build placeholders. It runs at construction time so any
// caller of NewRootCommand sees a populated --version output without
// duplicating the placeholder strings at every call site.
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

// HTTPDoer is the HTTP transport port. http.Client satisfies it; tests
// substitute a mock so the topup transport can be exercised without a live
// network. It is exported so mockery can generate the mock.
type HTTPDoer interface {
	// Do issues an HTTP request and returns the response or an error.
	Do(*http.Request) (*http.Response, error)
}

// KubeClientFactory constructs a kube.Client from the resolved kube.Config.
// The default factory (set in NewRootCommand) wraps kube.NewClient so the
// concrete adapter satisfies the port. Tests provide a factory that returns
// a mock.
type KubeClientFactory func(kube.Config) (kube.Client, error)

// KubeNamespaceResolver resolves the kubeconfig default namespace without
// opening a Kubernetes connection. It is split from KubeClientFactory so
// the --dry-run path can derive a namespace without dialling the cluster.
type KubeNamespaceResolver func(kube.Config) (string, error)

// Options customises root command construction. All fields are optional;
// nil fields are filled with the production defaults (stdin/stdout/stderr,
// a fresh Viper, the real kube.NewClient, http.DefaultClient).
type Options struct {
	// In, Out, Err are the command's standard streams.
	In  io.Reader
	Out io.Writer
	Err io.Writer

	// Build is the linker-injected version metadata.
	Build BuildInfo

	// Viper is the configuration registry. Tests typically pass a fresh
	// viper.New() to isolate from process-wide state.
	Viper *viper.Viper

	// KubeClientFactory constructs the Kubernetes adapter at run time.
	// Tests inject a factory that returns a mock.
	KubeClientFactory KubeClientFactory

	// KubeNamespaceResolver resolves the default namespace for the
	// --dry-run path, where no live client is constructed.
	KubeNamespaceResolver KubeNamespaceResolver

	// HTTPClient is the transport used by the topup faucet POST. Tests
	// inject a mock to capture the request and shape the response.
	HTTPClient HTTPDoer
}

// commandContext is the per-process runtime each subcommand reads at RunE
// time. It is constructed once by NewRootCommand from the fully-defaulted
// Options and passed by pointer to every command factory.
type commandContext struct {
	in                    io.Reader
	out                   io.Writer
	err                   io.Writer
	viper                 *viper.Viper
	kubeClientFactory     KubeClientFactory
	kubeNamespaceResolver KubeNamespaceResolver
	httpClient            HTTPDoer
	logger                *slog.Logger
}
