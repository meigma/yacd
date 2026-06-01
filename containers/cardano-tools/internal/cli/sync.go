package cli

import (
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/artifactsync"
)

// defaultSyncTimeout bounds each artifact download when --http-timeout is unset.
const defaultSyncTimeout = 60 * time.Second

// newSyncCommand builds the "sync" subcommand, which mirrors a served artifact
// bundle from a cardano-tools serve endpoint into a local directory, verifying
// every file against the served manifest.
func newSyncCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Mirror a served artifact bundle from a serve endpoint, verifying the manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vp := commandContext.viper
			timeout := vp.GetDuration("http-timeout")
			if timeout <= 0 {
				timeout = defaultSyncTimeout
			}
			opts := artifactsync.Options{
				ServeURL:  vp.GetString("serve-url"),
				OutputDir: vp.GetString("output-dir"),
				DryRun:    vp.GetBool("dry-run"),
			}
			// Refuse redirects: the serve endpoint returns artifacts directly,
			// so a redirect would silently move a download to another host.
			client := &http.Client{
				Timeout: timeout,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			return artifactsync.Run(cmd.Context(), opts, client, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	flags.String("serve-url", "", "Base URL of the cardano-tools serve endpoint to mirror")
	flags.String("output-dir", "/network-artifacts", "Directory the verified artifacts are written into")
	flags.Duration("http-timeout", defaultSyncTimeout, "Per-request download timeout")
	flags.Bool("dry-run", false, "Print the served manifest's file list and fetch nothing")

	return cmd
}
