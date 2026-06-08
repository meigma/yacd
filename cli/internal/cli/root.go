package cli

import (
	"io"
	"log/slog"
	"strings"

	"github.com/meigma/yacd/charts"
	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/cluster/k3d"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/clusterstate/file"
	"github.com/meigma/yacd/cli/internal/exec"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/meigma/yacd/cli/internal/operator/ssa"
	"github.com/meigma/yacd/cli/internal/toolbin"
	"github.com/meigma/yacd/cli/internal/toolbin/ghrelease"
	"github.com/meigma/yacd/internal/cardano/tx"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewRootCommand creates the YACD developer CLI command tree, defaulting
// any nil Options fields and wiring the persistent flags, viper binding,
// logger construction, and the up/down/list/info/wallet subcommands.
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
		options.KubeClientFactory = func(config kube.Config) (kube.Client, error) {
			return kube.NewClient(config)
		}
	}
	if options.UTxOConfirmerFactory == nil {
		options.UTxOConfirmerFactory = func(kupoURL string) UTxOConfirmer {
			return newKupoConfirmer(kupoURL)
		}
	}
	if options.TxSubmitterFactory == nil {
		options.TxSubmitterFactory = func(ogmiosURL string, kupoURL string) tx.Submitter {
			return tx.Apollo{OgmiosURL: ogmiosURL, KupoURL: kupoURL}
		}
	}
	if options.EndpointProber == nil {
		options.EndpointProber = probeEndpointReachable
	}
	if options.ClusterProvisionerFactory == nil {
		options.ClusterProvisionerFactory = func() (cluster.Provisioner, error) {
			dir, err := toolbin.DefaultDir()
			if err != nil {
				return nil, err
			}
			resolver := ghrelease.New(ghrelease.DefaultK3dPin, dir, ghrelease.DefaultHTTPClient())
			return k3d.New(resolver, exec.OS()), nil
		}
	}
	if options.OperatorInstallerFactory == nil {
		options.OperatorInstallerFactory = func(kubeconfig, kubeContext string) (operator.Installer, error) {
			return ssa.New(kubeconfig, kubeContext, charts.OperatorChart)
		}
	}
	if options.ClusterStateFactory == nil {
		options.ClusterStateFactory = func() (clusterstate.Store, error) {
			dir, err := clusterstate.DefaultDir()
			if err != nil {
				return nil, err
			}
			return file.New(dir), nil
		}
	}
	options.Build = options.Build.withDefaults()

	ctx := &commandContext{
		in:                   options.In,
		out:                  options.Out,
		err:                  options.Err,
		viper:                options.Viper,
		kubeClientFactory:    options.KubeClientFactory,
		utxoConfirmerFactory: options.UTxOConfirmerFactory,
		txSubmitterFactory:   options.TxSubmitterFactory,
		endpointProber:       options.EndpointProber,
		clusterProvisioner:   options.ClusterProvisionerFactory,
		operatorInstaller:    options.OperatorInstallerFactory,
		clusterState:         options.ClusterStateFactory,
		k3dVersion:           ghrelease.DefaultK3dPin.Version,
		build:                options.Build,
		logger:               slog.New(slog.NewTextHandler(options.Err, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	root := &cobra.Command{
		Use:           "yacd",
		Short:         "YACD developer CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := initializeConfig(cmd, ctx.viper); err != nil {
				return err
			}
			runtimeConfig, err := loadRuntimeConfig(ctx.viper)
			if err != nil {
				return err
			}
			// -v/-q are flag-only knobs read straight off cmd.Flags() so an
			// ambient YACD_VERBOSE/YACD_QUIET cannot raise verbosity or mute a
			// scripted run. -v is additive over the resolved --log-level; -q
			// forces the logger off entirely.
			verbosity, err := cmd.Flags().GetCount("verbose")
			if err != nil {
				return err
			}
			quiet, err := cmd.Flags().GetBool("quiet")
			if err != nil {
				return err
			}
			runtimeConfig.LogLevel = raise(runtimeConfig.LogLevel, verbosity)
			ctx.logger = newLogger(runtimeConfig, ctx.err, quiet)
			return nil
		},
	}
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)

	root.PersistentFlags().String("kubeconfig", "", "Path to the kubeconfig file")
	root.PersistentFlags().String("context", "", "Kubeconfig context to use")
	root.PersistentFlags().StringP("namespace", "n", "", "Kubernetes namespace")
	root.PersistentFlags().String("log-level", "info", "Log level: debug, info, warn, error")
	root.PersistentFlags().String("log-format", formatText, "Log format: text, json")
	// Session/TTY decisions. These are flag-only (no env tier by design) and are
	// read directly off cmd.Flags() wherever they are consumed, so no ambient
	// YACD_* can drive them or leak into a child environment.
	root.PersistentFlags().CountP("verbose", "v", "Increase log verbosity (-v, -vv, -vvv)")
	root.PersistentFlags().BoolP("quiet", "q", false, "Suppress progress, logs, and warnings; print only data and errors")
	root.PersistentFlags().Bool("non-interactive", false, "Never prompt; fail instead of waiting for input")
	root.PersistentFlags().String("color", "auto", "Color output: auto, always, never")
	root.PersistentFlags().Bool("no-color", false, "Disable color output")
	root.PersistentFlags().StringP("output", "o", formatText, "Output format: text, json")

	// The network verbs resolve their target through the shared resolver, so
	// each is wrapped to clear a stale managed-cluster record when it targeted
	// a managed cluster that has since disappeared. devnet manages the cluster
	// itself and reconciles directly.
	root.AddCommand(ctx.withManagedReconcile(newUpCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newDownCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newListCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newInfoCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newWalletCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newRunCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newExecCommand(ctx)))
	root.AddCommand(ctx.withManagedReconcile(newConnectCommand(ctx)))
	root.AddCommand(newDevnetCommand(ctx))
	// init only prints an embedded template — no cluster contact, so no reconcile.
	root.AddCommand(newInitCommand(ctx))
	// version replaces cobra's removed --version flag (dropped so -v is free for
	// verbosity); it prints build metadata to stdout and contacts nothing.
	root.AddCommand(newVersionCommand(ctx))
	// install targets an arbitrary cluster by explicit-or-ambient kubeconfig; it
	// deliberately accepts --kubeconfig/--context and never consults the managed
	// devnet record, so it is neither wrapped in withManagedReconcile nor gated
	// by rejectExplicitTarget.
	root.AddCommand(newInstallCommand(ctx))

	return root
}
