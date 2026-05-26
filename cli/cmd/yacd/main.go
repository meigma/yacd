// Command yacd is the YACD developer CLI binary entrypoint. It builds the
// cobra command tree from cli.NewRootCommand and exits with the appropriate
// status code after writing any error to stderr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/meigma/yacd/cli/internal/cli"
)

// version, commit, and date are linker-injected by GoReleaser at release
// time. Development builds keep the placeholder defaults.
//
//nolint:gochecknoglobals // GoReleaser injects these values with ldflags during releases.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run())
}

// run installs a SIGINT/SIGTERM-cancelled context, builds the root command,
// and executes it. A non-nil error is written to stderr; the exit status is
// 0 on success and 1 on any error.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	root := cli.NewRootCommand(cli.Options{
		In: os.Stdin,
		Build: cli.BuildInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
		Out: os.Stdout,
		Err: os.Stderr,
	})
	if err := root.ExecuteContext(ctx); err != nil {
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			return 1
		}

		return 1
	}

	return 0
}
