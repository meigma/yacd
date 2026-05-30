package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/generate"
	"github.com/meigma/yacd/internal/cardano/localnet"
)

// newGenerateCommand builds the "generate" subcommand, which shims
// cardano-testnet create-env to produce a localnet artifact environment.
func newGenerateCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a localnet artifact environment with cardano-testnet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vp := commandContext.viper
			opts := generate.Options{
				Spec: localnet.Spec{
					NetworkMagic: vp.GetInt64("network-magic"),
					PoolCount:    vp.GetInt("pool-count"),
					Timing: localnet.Timing{
						SlotLength:  vp.GetDuration("slot-length"),
						EpochLength: vp.GetInt("epoch-length"),
					},
					Paths: localnet.Paths{
						StateDir: vp.GetString("state-dir"),
						EnvDir:   vp.GetString("output-dir"),
					},
					Tool: localnet.Tool{
						Binary:  vp.GetString("cardano-testnet-binary"),
						Version: vp.GetString("tool-version"),
					},
				},
				CardanoCLI: vp.GetString("cardano-cli"),
				DryRun:     vp.GetBool("dry-run"),
			}
			return generate.Run(cmd.Context(), opts, cmd.OutOrStdout())
		},
	}

	// Flags default to zero values; internal/cardano/localnet.BuildPlan is the
	// single source of truth for the localnet defaults applied when unset.
	flags := cmd.Flags()
	flags.Int64("network-magic", 0, "Cardano testnet magic (default 42)")
	flags.Int("pool-count", 0, "Number of generated stake pool nodes (default 1)")
	flags.Int("epoch-length", 0, "Slots per epoch (default 500)")
	flags.Duration("slot-length", time.Duration(0), "Slot duration (default 100ms)")
	flags.String("state-dir", "", "Durable state mount root (default /state)")
	flags.String("output-dir", "", "Environment directory cardano-testnet populates (default /state/env)")
	flags.String("cardano-testnet-binary", "", "cardano-testnet command or path (default cardano-testnet)")
	flags.String("cardano-cli", "", "cardano-cli command or path used for genesis hashing (default CARDANO_CLI or cardano-cli)")
	flags.String("tool-version", "", "cardano-testnet release recorded in the plan manifest")
	flags.Bool("dry-run", false, "Print the create-env invocation and resolved layout, generating nothing")

	return cmd
}
