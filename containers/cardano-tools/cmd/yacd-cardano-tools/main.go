// Command yacd-cardano-tools is the single utility YACD uses for Cardano
// network artifact operations: generating localnet artifacts, fetching public
// network artifacts from trusted sources, serving an artifact directory over
// HTTP, and reporting an artifact set's manifest into the network ConfigMap.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/meigma/yacd/containers/cardano-tools/internal/cli"
)

// Build metadata populated at link time via -ldflags. The defaults are used
// when running from source.
//
//nolint:gochecknoglobals // the release build injects these values with ldflags.
var (
	// version is the semantic version of the binary or "dev" from source.
	version = "dev"
	// commit is the source revision the binary was built from or "none".
	commit = "none"
	// date is the build timestamp or "unknown" from source.
	date = "unknown"
)

// main delegates exit-code management to [run] so deferred cleanup (the
// signal-handler teardown) runs before the process exits.
func main() {
	os.Exit(run())
}

// run builds the root command, wires it to a SIGINT/SIGTERM-cancelled context,
// and executes it. run returns 0 on success and 1 on any execution error,
// writing the error to stderr.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	root := cli.NewRootCommand(cli.Options{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		Build: cli.BuildInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
	})
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}
