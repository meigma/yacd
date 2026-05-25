package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(commandContext *commandContext, build BuildInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s) built %s\n", binaryName, build.Version, build.Commit, build.Date)
			return err
		},
	}

	_ = commandContext
	return cmd
}
