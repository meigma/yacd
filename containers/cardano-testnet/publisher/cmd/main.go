// Command yacd-cardano-testnet-publisher publishes generated Cardano
// localnet artifacts into the network artifact ConfigMap that
// downstream YACD controllers consume.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/meigma/yacd/containers/cardano-testnet/publisher/internal/cli"
)

// Build metadata populated at link time by GoReleaser via -ldflags.
// The defaults are used when running from source.
//
//nolint:gochecknoglobals // GoReleaser injects these values with ldflags during releases.
var (
	// version is the semantic version of the binary or "dev" when running
	// from source.
	version = "dev"
	// commit is the source revision the binary was built from or "none"
	// when running from source.
	commit = "none"
	// date is the build timestamp or "unknown" when running from source.
	date = "unknown"
)

// main is the program entry point.
//
// main delegates exit code management to [run] so deferred cleanup
// (signal-handler teardown) runs before the process exits.
func main() {
	os.Exit(run())
}

// run builds the root Cobra command, wires it to a signal-aware context,
// and executes it. run returns 0 on success and 1 on any execution error.
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
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			return 1
		}

		return 1
	}

	return 0
}
