package cli

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewRootCommand creates the YACD developer CLI command tree, defaulting
// any nil Options fields and wiring the persistent flags, viper binding,
// logger construction, and the up/down/list/info/topup subcommands.
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
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.UTxOConfirmerFactory == nil {
		options.UTxOConfirmerFactory = func(kupoURL string) UTxOConfirmer {
			return newKupoConfirmer(kupoURL)
		}
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
			return ssa.New(kubeconfig, kubeContext, ssa.Manifests)
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
		httpClient:           options.HTTPClient,
		utxoConfirmerFactory: options.UTxOConfirmerFactory,
		clusterProvisioner:   options.ClusterProvisionerFactory,
		operatorInstaller:    options.OperatorInstallerFactory,
		clusterState:         options.ClusterStateFactory,
		k3dVersion:           ghrelease.DefaultK3dPin.Version,
		logger:               slog.New(slog.NewTextHandler(options.Err, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	root := &cobra.Command{
		Use:           "yacd",
		Short:         "YACD developer CLI",
		Version:       options.Build.Version,
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
			ctx.logger = newLogger(runtimeConfig, ctx.err)
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

	root.AddCommand(newUpCommand(ctx))
	root.AddCommand(newDownCommand(ctx))
	root.AddCommand(newListCommand(ctx))
	root.AddCommand(newInfoCommand(ctx))
	root.AddCommand(newTopUpCommand(ctx))
	root.AddCommand(newRunCommand(ctx))
	root.AddCommand(newExecCommand(ctx))
	root.AddCommand(newConnectCommand(ctx))
	root.AddCommand(newDevnetCommand(ctx))

	return root
}
