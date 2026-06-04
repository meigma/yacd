package cli

import (
	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/genesisfund"
)

// defaultEnvDir matches the cardano-testnet create-env environment directory
// the generate verb populates by default, so fund-genesis can run against a
// freshly generated localnet without an explicit --env-dir.
const defaultEnvDir = "/state/env"

// newFundGenesisCommand builds the "fund-genesis" subcommand, which adds an
// initialFunds allocation for a bech32 address to a local Shelley genesis. It
// replaces the operator's previous sed/grep/bech32 init-container pipeline for
// funding a well-known faucet wallet at genesis.
func newFundGenesisCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fund-genesis",
		Short: "Add an initialFunds allocation for an address to a local Shelley genesis",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vp := commandContext.viper
			return genesisfund.Run(cmd.Context(), genesisfund.Options{
				EnvDir:   vp.GetString("env-dir"),
				Address:  vp.GetString("address"),
				Lovelace: vp.GetInt64("lovelace"),
			}, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	flags.String("env-dir", defaultEnvDir, "Directory containing shelley-genesis.json")
	flags.String("address", "", "Bech32 Cardano testnet address to fund (addr_test1...)")
	flags.Int64("lovelace", 0, "Positive lovelace amount to allocate at genesis")

	return cmd
}
