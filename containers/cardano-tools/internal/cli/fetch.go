package cli

import (
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/fetch"
)

// defaultFetchTimeout bounds each artifact download when --http-timeout is
// unset.
const defaultFetchTimeout = 60 * time.Second

// newFetchCommand builds the "fetch" subcommand, which downloads a public
// network's artifacts from trusted sources with pinned integrity.
func newFetchCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download a public network's artifacts from trusted sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vp := commandContext.viper
			timeout := vp.GetDuration("http-timeout")
			if timeout <= 0 {
				timeout = defaultFetchTimeout
			}
			opts := fetch.Options{
				Profile:   vp.GetString("profile"),
				OutputDir: vp.GetString("output-dir"),
				DryRun:    vp.GetBool("dry-run"),
			}
			return fetch.Run(cmd.Context(), opts, &http.Client{Timeout: timeout}, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	flags.String("profile", "", "Public network profile to fetch (preview, preprod, mainnet)")
	flags.String("output-dir", "/profile", "Directory the downloaded artifacts are written into")
	flags.Duration("http-timeout", defaultFetchTimeout, "Per-request download timeout")
	flags.Bool("dry-run", false, "Print the resolved download manifest and fetch nothing")

	return cmd
}
