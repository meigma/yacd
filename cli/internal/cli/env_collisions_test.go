package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChainOverridesFromFlagsIgnoresEnv(t *testing.T) {
	// An ambient YACD_OGMIOS_URL / YACD_KUPO_URL must not shadow the funding
	// flags: reading them off cmd.Flags() (not viper) is the fix.
	t.Setenv("YACD_OGMIOS_URL", "ws://evil:1")
	t.Setenv("YACD_KUPO_URL", "http://evil:2")

	cmd := &cobra.Command{}
	addChainOverrideFlags(cmd)
	require.NoError(t, cmd.ParseFlags(nil))

	overrides, err := chainOverridesFromFlags(cmd)
	require.NoError(t, err)
	assert.Empty(t, overrides.OgmiosURL, "YACD_OGMIOS_URL must not shadow --ogmios-url")
	assert.Empty(t, overrides.KupoURL, "YACD_KUPO_URL must not shadow --kupo-url")
}

func TestChainOverridesFromFlagsReadsFlags(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	addChainOverrideFlags(cmd)
	require.NoError(t, cmd.ParseFlags([]string{"--ogmios-url", "ws://host:1", "--kupo-url", "http://host:2"}))

	overrides, err := chainOverridesFromFlags(cmd)
	require.NoError(t, err)
	assert.Equal(t, "ws://host:1", overrides.OgmiosURL)
	assert.Equal(t, "http://host:2", overrides.KupoURL)
}

// TestTimeoutFlagIgnoresEnvBleed demonstrates the cross-command bleed bug and
// its fix at the mechanism level: viper (the old read path) is shadowed by
// YACD_TIMEOUT, while cmd.Flags() (the new read path) is immune.
func TestTimeoutFlagIgnoresEnvBleed(t *testing.T) {
	t.Setenv("YACD_TIMEOUT", "1s")

	cmd := &cobra.Command{}
	cmd.Flags().Duration("timeout", 12*time.Minute, "")
	require.NoError(t, cmd.ParseFlags(nil))

	vp := viper.New()
	vp.SetEnvPrefix("YACD")
	vp.AutomaticEnv()
	require.NoError(t, vp.BindPFlags(cmd.Flags()))

	assert.Equal(t, time.Second, vp.GetDuration("timeout"),
		"the old viper read path is shadowed by YACD_TIMEOUT (the bug)")

	got, err := cmd.Flags().GetDuration("timeout")
	require.NoError(t, err)
	assert.Equal(t, 12*time.Minute, got, "cmd.Flags() is immune to YACD_TIMEOUT (the fix)")
}

// TestHostEnvExcludesUXKeys guards repo invariant D: the CLI-injected child env
// (run.go appends connected.env to os.Environ) is built from scratch and carries
// only the documented host-access keys, never the flag-only UX keys — even when
// those are exported in the process environment.
func TestHostEnvExcludesUXKeys(t *testing.T) {
	t.Setenv("YACD_OUTPUT", "json")
	t.Setenv("YACD_VERBOSE", "3")
	t.Setenv("YACD_QUIET", "true")

	env := hostEnvFromURLs(readyNetwork("devnet"), "ws://127.0.0.1:40001", "http://127.0.0.1:40002")

	for _, entry := range env {
		for _, banned := range []string{"YACD_OUTPUT=", "YACD_VERBOSE=", "YACD_QUIET=", "YACD_NON_INTERACTIVE=", "YACD_COLOR="} {
			assert.False(t, strings.HasPrefix(entry, banned),
				"CLI-injected env must not carry the flag-only UX key %q", banned)
		}
	}
}
