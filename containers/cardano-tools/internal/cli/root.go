package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// binaryName is the public name of the executable used in the root command's
// Use string, the version template, and the version subcommand output.
const binaryName = "yacd-cardano-tools"

// BuildInfo describes linker-injected build metadata surfaced through
// --version and the version subcommand.
type BuildInfo struct {
	// Version is the semantic version of the binary, e.g. "1.2.3" or "dev"
	// when running from source.
	Version string
	// Commit is the source revision the binary was built from, e.g. a short
	// Git SHA, or "none" when unknown.
	Commit string
	// Date is the build timestamp, typically in RFC3339 form, or "unknown"
	// when not set.
	Date string
}

// Options customizes [NewRootCommand] construction. Zero-valued fields receive
// sensible defaults so callers can override only what they need.
type Options struct {
	// In is the reader used for any subcommand stdin. Nil selects an empty
	// reader.
	In io.Reader
	// Out is the writer used for subcommand stdout. Nil selects [io.Discard].
	Out io.Writer
	// Err is the writer used for subcommand stderr. Nil selects [io.Discard].
	Err io.Writer
	// Build provides linker-injected version metadata. Missing fields are
	// replaced with their development defaults.
	Build BuildInfo
	// Viper is the Viper instance used to bind flags and resolve environment
	// variables. Nil constructs a fresh, unbound instance.
	Viper *viper.Viper
}

// commandContext carries the shared dependencies that subcommands need at
// construction and runtime.
type commandContext struct {
	// in is the input reader threaded through to subcommands that need it.
	in io.Reader
	// out is the standard output writer used by subcommands.
	out io.Writer
	// err is the standard error writer used by subcommands.
	err io.Writer
	// viper is the shared Viper instance every subcommand's PersistentPreRunE
	// reads from for both flag and YACD_* env-var sources.
	viper *viper.Viper
}

// NewRootCommand constructs the yacd-cardano-tools Cobra command tree.
//
// The returned command wires PersistentPreRunE to [initializeConfig] so the
// active subcommand's flags are bound to Viper before its RunE executes.
func NewRootCommand(options Options) *cobra.Command {
	if options.In == nil {
		options.In = strings.NewReader("")
	}
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.Err == nil {
		options.Err = io.Discard
	}
	if options.Viper == nil {
		options.Viper = viper.New()
	}
	options.Build = options.Build.withDefaults()

	commandContext := &commandContext{
		in:    options.In,
		out:   options.Out,
		err:   options.Err,
		viper: options.Viper,
	}

	root := &cobra.Command{
		Use:           binaryName,
		Short:         "Acquire, verify, and serve Cardano network artifacts",
		Version:       options.Build.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initializeConfig(cmd, commandContext.viper)
		},
	}
	root.SetVersionTemplate(fmt.Sprintf("%s %s (%s) built %s\n", binaryName, options.Build.Version, options.Build.Commit, options.Build.Date))
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)

	root.AddCommand(newGenerateCommand(commandContext))
	root.AddCommand(newFetchCommand(commandContext))
	root.AddCommand(newServeCommand(commandContext))
	root.AddCommand(newReportCommand(commandContext))
	root.AddCommand(newStageCommand(commandContext))
	root.AddCommand(newVersionCommand(options.Build))

	return root
}

// withDefaults returns a copy of b with empty fields replaced by their
// development defaults ("dev", "none", "unknown").
func (b BuildInfo) withDefaults() BuildInfo {
	if strings.TrimSpace(b.Version) == "" {
		b.Version = "dev"
	}
	if strings.TrimSpace(b.Commit) == "" {
		b.Commit = "none"
	}
	if strings.TrimSpace(b.Date) == "" {
		b.Date = "unknown"
	}
	return b
}

// initializeConfig sets the YACD environment prefix and a "-"/"."-to-"_" key
// replacer on vp, enables AutomaticEnv, and binds cmd.Flags to vp. A flag
// named foo-bar is then resolvable from the YACD_FOO_BAR environment variable.
func initializeConfig(cmd *cobra.Command, vp *viper.Viper) error {
	vp.SetEnvPrefix("YACD")
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	if err := vp.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("bind command flags: %w", err)
	}

	return nil
}
