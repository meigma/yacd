package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newInitCommand wires the `yacd init` subcommand. It prints a fully-commented
// developer Environment template to stdout; the active portion is a ready-to-run
// local network, and commented blocks document the rest of the API. Output goes
// to stdout so it composes with a redirect: `yacd init > yacd.yaml`.
func newInitCommand(commandContext *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Print a commented yacd.yaml environment template",
		Long: `Print a fully-commented developer environment template to stdout.

The active configuration is a ready-to-run local devnet with a genesis-funded
wallet; commented blocks document the rest of the API. Redirect it to a file
and apply it:

  yacd init > yacd.yaml
  yacd up dev -f yacd.yaml`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := commandContext.out.Write(defaultInitEnvYAML); err != nil {
				return fmt.Errorf("write environment template: %w", err)
			}

			return nil
		},
	}
}
