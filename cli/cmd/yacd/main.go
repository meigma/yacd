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
// and executes it. The exit status comes from cli.ResolveExit so the run and
// exec verbs can propagate a child or in-pod process's exit code; ordinary
// errors are written to stderr and exit 1, while a code carried by an already-
// reported process exit is returned without a duplicate message.
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

	err := root.ExecuteContext(ctx)
	code, printErr := cli.ResolveExit(err)
	if printErr {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}

	return code
}
