package ui

import (
	"context"
	"fmt"
	"io"

	"github.com/meigma/yacd/cli/internal/lifecycle"
)

// Reporter selects the progress reporter for long-running work. Quiet selects
// the no-op reporter, which still runs the action but emits nothing; otherwise
// the plain reporter prints stepwise lines to the human plane. The animated
// reporter and its TTY/interactive/color predicate arrive in the styling phase;
// this interim has no charm path.
func (i IO) Reporter() lifecycle.Reporter {
	if i.cfg.Quiet {
		return lifecycle.NopReporter{}
	}
	return &plainReporter{w: i.errw}
}

// plainReporter writes lifecycle progress to the human plane with "==> " for
// top-level steps and "    " for sub-steps and completions, byte-identical to
// the command layer's existing stepReporter so live and Chainsaw phrase
// assertions stay valid as routing migrates onto this reporter.
type plainReporter struct {
	w io.Writer
}

// Step implements lifecycle.Reporter.
func (r *plainReporter) Step(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "==> "+format+"\n", args...)
}

// Substep implements lifecycle.Reporter.
func (r *plainReporter) Substep(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "    "+format+"\n", args...)
}

// Done implements lifecycle.Reporter.
func (r *plainReporter) Done(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "    "+format+"\n", args...)
}

// Run implements lifecycle.Reporter; it runs action and returns its error,
// writing nothing and discarding the title. The command layer reports
// completion with Done after Run.
func (r *plainReporter) Run(ctx context.Context, _ string, action func(context.Context) error) error {
	return action(ctx)
}
