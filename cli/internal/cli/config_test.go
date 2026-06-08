package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/meigma/yacd/cli/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// globalFlagsCommand returns a command carrying the global persistent flags,
// parsed from args, so loadRuntimeConfig and the resolution helpers can be
// exercised without standing up the whole root command. Precedence-bearing
// values (log-level, output) are driven through viper in those tests; the
// flag-only knobs come from these parsed flags.
func globalFlagsCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{RunE: func(*cobra.Command, []string) error { return nil }}
	addGlobalFlags(cmd.Flags())
	require.NoError(t, cmd.ParseFlags(args))
	return cmd
}

func TestRaise(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		base    string
		verbose int
		want    string
	}{
		{"no verbose keeps base", "info", 0, "info"},
		{"info plus one is debug", "info", 1, "debug"},
		{"info caps at debug", "info", 5, "debug"},
		{"warn steps to info", "warn", 1, "info"},
		{"warn two steps to debug", "warn", 2, "debug"},
		{"error three steps to debug", "error", 3, "debug"},
		{"debug never lowers", "debug", 2, "debug"},
		{"warn unchanged without verbose", "warn", 0, "warn"},
		{"unknown base returned unchanged", "trace", 2, "trace"},
		{"negative verbose returned unchanged", "info", -1, "info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, raise(tc.base, tc.verbose))
		})
	}
}

func TestSlogLevel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, slog.LevelDebug, slogLevel("debug"))
	assert.Equal(t, slog.LevelInfo, slogLevel("info"))
	assert.Equal(t, slog.LevelWarn, slogLevel("warn"))
	assert.Equal(t, slog.LevelError, slogLevel("error"))
	assert.Equal(t, slog.LevelInfo, slogLevel("unknown"))
}

func TestResolveColorMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		value        string
		noColor      bool
		colorChanged bool
		want         ui.ColorMode
		wantErr      bool
	}{
		{"auto default", "auto", false, false, ui.ColorAuto, false},
		{"empty is auto", "", false, false, ui.ColorAuto, false},
		{"always", "always", false, true, ui.ColorAlways, false},
		{"never", "never", false, true, ui.ColorNever, false},
		{"no-color forces never", "auto", true, false, ui.ColorNever, false},
		{"explicit color beats no-color", "always", true, true, ui.ColorAlways, false},
		{"unknown errors", "purple", false, true, ui.ColorAuto, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveColorMode(tc.value, tc.noColor, tc.colorChanged)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveUXNonTTYBuffers(t *testing.T) {
	t.Parallel()

	// Buffers are non-TTY, so default (auto) color is off and the latch forces
	// non-interactive — the determinism backstop tests rely on.
	color, nonInteractive := resolveUX(RuntimeConfig{Color: ui.ColorAuto}, &bytes.Buffer{}, strings.NewReader(""))
	assert.False(t, color, "auto color is off on a non-TTY stream")
	assert.True(t, nonInteractive, "a non-TTY stream is always non-interactive")
}

func TestResolveUXColorAlwaysForcesColor(t *testing.T) {
	// Neutralise any ambient NO_COLOR (CI sets it) so this asserts the
	// --color=always override alone. Not parallel: it mutates the environment.
	t.Setenv("NO_COLOR", "")

	// --color=always is an explicit override: it forces color even off-TTY
	// (design §2.4); only NO_COLOR overrides it.
	color, _ := resolveUX(RuntimeConfig{Color: ui.ColorAlways}, &bytes.Buffer{}, strings.NewReader(""))
	assert.True(t, color)
}

func TestResolveUXNoColorEnvIsSupreme(t *testing.T) {
	// NO_COLOR beats --color=always.
	t.Setenv("NO_COLOR", "1")

	color, _ := resolveUX(RuntimeConfig{Color: ui.ColorAlways}, &bytes.Buffer{}, strings.NewReader(""))
	assert.False(t, color, "NO_COLOR is supreme over --color=always")
}

func TestResolveUXNonInteractiveFlag(t *testing.T) {
	t.Parallel()

	_, nonInteractive := resolveUX(RuntimeConfig{NonInteractive: true}, &bytes.Buffer{}, strings.NewReader(""))
	assert.True(t, nonInteractive)
}

func TestLoadRuntimeConfigResolvesFlags(t *testing.T) {
	t.Parallel()

	cmd := globalFlagsCommand(t, "-vv", "-q", "--non-interactive")
	vp := viper.New()
	vp.Set("log-level", "warn")
	vp.Set("output", "json")

	config, err := loadRuntimeConfig(cmd, vp)
	require.NoError(t, err)

	assert.Equal(t, "debug", config.LogLevel, "warn raised two steps by -vv")
	assert.Equal(t, 2, config.Verbosity)
	assert.True(t, config.Quiet)
	assert.True(t, config.NonInteractive)
	assert.Equal(t, formatJSON, config.OutputFormat)
}

func TestLoadRuntimeConfigRejectsUnknownColor(t *testing.T) {
	t.Parallel()

	cmd := globalFlagsCommand(t, "--color", "purple")
	_, err := loadRuntimeConfig(cmd, viper.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported color mode")
}
