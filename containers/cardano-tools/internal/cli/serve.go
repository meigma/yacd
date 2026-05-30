package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/serve"
)

// newServeCommand builds the "serve" subcommand, which exposes an artifact
// directory over HTTP for out-of-cluster consumers.
func newServeCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve an artifact directory read-only over HTTP",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vp := commandContext.viper
			return serve.Run(cmd.Context(), serve.Options{
				Dir:               vp.GetString("artifacts-dir"),
				Listen:            vp.GetString("listen"),
				ReadHeaderTimeout: vp.GetDuration("read-header-timeout"),
			}, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	flags.String("artifacts-dir", "", "Directory of non-secret artifacts to serve (required)")
	flags.String("listen", ":8080", "TCP address to listen on")
	flags.Duration("read-header-timeout", 10*time.Second, "Maximum duration for reading request headers")

	return cmd
}
