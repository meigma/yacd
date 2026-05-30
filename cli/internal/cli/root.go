package cli

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/meigma/yacd/cli/internal/kube"
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
	options.Build = options.Build.withDefaults()

	ctx := &commandContext{
		in:                options.In,
		out:               options.Out,
		err:               options.Err,
		viper:             options.Viper,
		kubeClientFactory: options.KubeClientFactory,
		httpClient:        options.HTTPClient,
		logger:            slog.New(slog.NewTextHandler(options.Err, &slog.HandlerOptions{Level: slog.LevelInfo})),
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

	return root
}
