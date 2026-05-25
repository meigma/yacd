package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCommand builds the "version" subcommand.
//
// The command writes a single line to stdout in the form
//
//	<binary> <version> (<commit>) built <date>
//
// using the [BuildInfo] supplied at root construction. This is the
// scriptable counterpart to the root command's --version flag, which
// renders the same information via its version template.
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
