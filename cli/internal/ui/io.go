package ui

import (
	"io"
	"os"

	"golang.org/x/term"
)

// IO is the single terminal-IO surface for the CLI. out is the data plane
// (stdout, never styled); errw is the human plane (stderr). In this charm-free
// interim color is a stored verdict with no styling attached yet, so the human
// plane is always plain text and no escape byte can reach a pipe; the styling
// phase wires color to real ANSI behind these same methods.
type IO struct {
	in       io.Reader
	out      io.Writer
	errw     io.Writer
	cfg      Config
	outIsTTY bool
	errIsTTY bool
	color    bool
}

// New builds an IO from the resolved Config and the injected streams. The TTY
// verdicts come from the concrete writers, so a bytes.Buffer is non-TTY and
// tests are deterministic. color is taken from cfg and is the single source of
// truth the styling phase consults.
func New(cfg Config, in io.Reader, out, errw io.Writer) IO {
	return IO{
		in:       in,
		out:      out,
		errw:     errw,
		cfg:      cfg,
		outIsTTY: IsTerminal(out),
		errIsTTY: IsTerminal(errw),
		color:    cfg.Color,
	}
}

// IsTerminal reports whether w is backed by a terminal file descriptor. It is
// the one TTY definition for the whole CLI.
func IsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// Color reports whether styled output is enabled.
func (i IO) Color() bool { return i.color }

// JSON reports whether the data plane should emit JSON.
func (i IO) JSON() bool { return i.cfg.OutputJSON }

// Quiet reports whether the human and progress plane is muted.
func (i IO) Quiet() bool { return i.cfg.Quiet }

// ErrIsTTY reports whether the human stream is a terminal.
func (i IO) ErrIsTTY() bool { return i.errIsTTY }

// Interactive reports whether blocking prompts are permitted: only when not
// NonInteractive and both stdin and the human stream are terminals. It is false
// whenever a prompt could hang a script.
func (i IO) Interactive() bool {
	return !i.cfg.NonInteractive && i.errIsTTY && isTerminalReader(i.in)
}

// isTerminalReader reports whether r is backed by a terminal file descriptor.
func isTerminalReader(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
