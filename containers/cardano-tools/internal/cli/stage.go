package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/artifactset"
	"github.com/meigma/yacd/containers/cardano-tools/internal/config"
	"github.com/meigma/yacd/containers/cardano-tools/internal/stage"
)

// newStageCommand builds the "stage" subcommand, which flattens a localnet
// cardano-testnet create-env state directory into a complete flat served
// directory: the contract-key artifact files, connection.json, and an integrity
// manifest.json. It is the local-mode counterpart of fetch, which produces the
// same served-directory shape for public profiles.
func newStageCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stage",
		Short: "Flatten a generated localnet state directory into a served artifact directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadStage(commandContext.viper)
			if err != nil {
				return err
			}
			return runStage(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	flags.String("state-dir", "", "cardano-testnet create-env state directory to flatten")
	flags.String("plan-manifest-file", "", "Absolute path to the localnet plan manifest (defaults to <state-dir>/yacd-localnet-plan.json)")
	flags.String("output-dir", "", "Directory the flat served artifacts are written into")
	flags.String("cardano-network-name", "", "Name of the owning CardanoNetwork resource")
	flags.String("cardano-network-namespace", "", "Namespace of the owning CardanoNetwork resource")
	flags.String("cardano-network-mode", "", "Network mode (e.g. local)")
	flags.String("cardano-network-era", "", "Cardano era for the staged connection metadata")
	flags.String("cardano-node-to-node-host", "", "Primary node-to-node Service hostname")
	flags.Int("cardano-node-to-node-port", 0, "Primary node-to-node Service port")
	flags.String("cardano-node-to-node-url", "", "Pre-built primary node-to-node URL (defaults to tcp://<host>:<port>)")
	flags.Bool("dry-run", false, "Print the files stage would write and write nothing")

	return cmd
}

// runStage maps the validated config onto the stage options and runs it.
func runStage(ctx context.Context, cfg config.StageConfig, out io.Writer) error {
	return stage.Run(ctx, stage.Options{
		StateDir:         cfg.StateDir,
		PlanManifestFile: cfg.PlanManifestFile,
		OutputDir:        cfg.OutputDir,
		Network: artifactset.NetworkIdentity{
			Name:           cfg.CardanoNetworkName,
			Namespace:      cfg.CardanoNetworkNamespace,
			Mode:           cfg.CardanoNetworkMode,
			Era:            cfg.CardanoNetworkEra,
			NodeToNodeHost: cfg.CardanoNodeToNodeHost,
			NodeToNodePort: cfg.CardanoNodeToNodePort,
			NodeToNodeURL:  cfg.CardanoNodeToNodeURL,
		},
		DryRun: cfg.DryRun,
	}, out)
}
