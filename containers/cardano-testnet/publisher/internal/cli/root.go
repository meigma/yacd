package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const binaryName = "yacd-cardano-testnet-publisher"

// BuildInfo describes linker-injected build metadata.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Options customizes root command construction.
type Options struct {
	In    io.Reader
	Out   io.Writer
	Err   io.Writer
	Build BuildInfo
	Viper *viper.Viper
}

type commandContext struct {
	in    io.Reader
	out   io.Writer
	err   io.Writer
	viper *viper.Viper
}

// NewRootCommand creates the publisher command tree.
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
		Short:         "Publish generated Cardano localnet artifacts to a ConfigMap",
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

	root.AddCommand(newPublishCommand(commandContext))
	root.AddCommand(newVersionCommand(commandContext, options.Build))

	return root
}

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

func initializeConfig(cmd *cobra.Command, vp *viper.Viper) error {
	vp.SetEnvPrefix("YACD")
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	if err := vp.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("bind command flags: %w", err)
	}

	return nil
}
