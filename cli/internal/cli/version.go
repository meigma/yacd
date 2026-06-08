package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCommand prints the build metadata to stdout. It replaces cobra's
// removed --version flag, so -v stays free for verbosity, and is the only
// surface for the version string. The output matches the previous --version
// template byte-for-byte.
func newVersionCommand(commandContext *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the yacd version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(commandContext.out, "yacd %s (%s) built %s\n",
				commandContext.build.Version, commandContext.build.Commit, commandContext.build.Date)
			return err
		},
	}
}
