package cli

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/meigma/yacd/internal/cardano/tx"
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

// KubeClientFactory constructs a kube.Client from the resolved kube.Config.
// The default factory (set in NewRootCommand) wraps kube.NewClient so the
// concrete adapter satisfies the port. Tests provide a factory that returns
// a mock.
type KubeClientFactory func(kube.Config) (kube.Client, error)

// UTxOConfirmer is the chain-index port topup --await polls to learn whether a
// funding transaction has been included. It is exported so mockery can generate
// the mock. The default implementation queries Kupo.
type UTxOConfirmer interface {
	// TransactionIDs returns the transaction IDs of the unspent outputs
	// currently at address.
	TransactionIDs(ctx context.Context, address string) ([]string, error)
}

// UTxOConfirmerFactory constructs a UTxOConfirmer for a Kupo base URL. The
// default factory (set in NewRootCommand) wraps the Kupo client; tests inject a
// factory that returns a mock.
type UTxOConfirmerFactory func(kupoURL string) UTxOConfirmer

// TxSubmitterFactory constructs the funding-transaction submitter for a pair of
// forwarded Ogmios and Kupo URLs. The default factory (set in NewRootCommand)
// builds a tx.Apollo bound to those loopback URLs; tests inject a factory that
// returns a mock so the wallet funding verbs are exercised without a live chain.
//
// It is a factory rather than a single submitter because the URLs are only known
// at run time, after the wallet command forwards Ogmios and Kupo.
type TxSubmitterFactory func(ogmiosURL string, kupoURL string) tx.Submitter

// EndpointProber reports whether a published externalURL is reachable, returning
// nil when it is. The endpoint resolver consults it before trusting an
// operator-asserted externalURL: a stale or wrong URL fails the probe and the
// resolver falls back to a port-forward. The default (set in NewRootCommand) does
// a short scheme-agnostic TCP dial to the URL's host:port; tests inject a stub.
type EndpointProber func(ctx context.Context, rawURL string) error

// ClusterProvisionerFactory constructs the managed-cluster provisioner. The
// default factory (set in NewRootCommand) resolves the pinned k3d binary and
// wires the k3d adapter; tests inject a factory that returns a mock. It is a
// factory so the error-prone binary/dir resolution is deferred to run time.
type ClusterProvisionerFactory func() (cluster.Provisioner, error)

// OperatorInstallerFactory constructs an operator installer bound to a
// kubeconfig and context. The default factory (set in NewRootCommand) wraps the
// SSA adapter with the embedded chart; tests inject a factory that returns a
// mock.
type OperatorInstallerFactory func(kubeconfig, kubeContext string) (operator.Installer, error)

// ClusterStateFactory constructs the managed-cluster state store. The default
// factory (set in NewRootCommand) resolves the XDG state dir and wraps the file
// adapter; tests inject a factory that returns a mock. It is a factory so the
// error-prone dir resolution is deferred to run time.
type ClusterStateFactory func() (clusterstate.Store, error)

// Options customises root command construction. All fields are optional;
// nil fields are filled with the production defaults (stdin/stdout/stderr,
// a fresh Viper, the real kube.NewClient).
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

	// UTxOConfirmerFactory constructs the chain-index confirmer used by
	// wallet funding --await. Tests inject a factory that returns a mock.
	UTxOConfirmerFactory UTxOConfirmerFactory

	// TxSubmitterFactory constructs the funding-transaction submitter used by
	// `wallet add --topup` and `wallet topup`. Tests inject a factory that
	// returns a mock so funding never touches a live chain.
	TxSubmitterFactory TxSubmitterFactory

	// EndpointProber decides whether a published externalURL is reachable, used
	// by the endpoint resolver to prefer a direct URL over a port-forward. Tests
	// inject a stub to drive the probe verdict.
	EndpointProber EndpointProber

	// ClusterProvisionerFactory constructs the managed-cluster provisioner
	// used by `devnet`. Tests inject a factory that returns a mock.
	ClusterProvisionerFactory ClusterProvisionerFactory

	// OperatorInstallerFactory constructs the operator installer used by
	// `devnet`. Tests inject a factory that returns a mock.
	OperatorInstallerFactory OperatorInstallerFactory

	// ClusterStateFactory constructs the managed-cluster state store used by
	// `devnet` and the shared target resolver. Tests inject a factory that
	// returns a mock.
	ClusterStateFactory ClusterStateFactory
}

// commandContext is the per-process runtime each subcommand reads at RunE
// time. It is constructed once by NewRootCommand from the fully-defaulted
// Options and passed by pointer to every command factory.
type commandContext struct {
	in                   io.Reader
	out                  io.Writer
	err                  io.Writer
	viper                *viper.Viper
	kubeClientFactory    KubeClientFactory
	utxoConfirmerFactory UTxOConfirmerFactory
	txSubmitterFactory   TxSubmitterFactory
	endpointProber       EndpointProber
	clusterProvisioner   ClusterProvisionerFactory
	operatorInstaller    OperatorInstallerFactory
	clusterState         ClusterStateFactory
	k3dVersion           string
	// managedEngaged records, for the duration of one command, whether the
	// shared target resolver fell through to the tracked managed devnet
	// cluster. The managed-reconcile wrapper reads it to decide whether a
	// failure warrants an orphaned-state check.
	managedEngaged bool
	logger         *slog.Logger
}
