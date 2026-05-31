package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseStageViper returns a Viper instance pre-populated with the minimum valid
// stage inputs. Tests override individual keys to exercise derivation and
// validation branches.
func baseStageViper(t *testing.T) *viper.Viper {
	t.Helper()
	vp := viper.New()
	vp.Set("state-dir", "/state/env")
	vp.Set("output-dir", "/served")
	vp.Set("cardano-network-name", "demo")
	vp.Set("cardano-network-namespace", "dev")
	vp.Set("cardano-network-mode", "local")
	vp.Set("cardano-network-era", "conway")
	vp.Set("cardano-node-to-node-host", "demo-node.dev.svc.cluster.local")
	vp.Set("cardano-node-to-node-port", 3001)
	return vp
}

func TestLoadStageDerivations(t *testing.T) {
	cfg, err := LoadStage(baseStageViper(t))
	require.NoError(t, err)

	// PlanManifestFile is left empty so the stage package derives it against the
	// resolved absolute state directory; config does not pre-derive it.
	assert.Empty(t, cfg.PlanManifestFile, "manifest path is not derived in config")
	assert.Equal(t, "tcp://demo-node.dev.svc.cluster.local:3001", cfg.CardanoNodeToNodeURL, "url synthesizes from host:port")
	assert.False(t, cfg.DryRun)
}

func TestLoadStageHonorsExplicitManifestFile(t *testing.T) {
	vp := baseStageViper(t)
	vp.Set("plan-manifest-file", "/state/env/yacd-localnet-plan.json")
	cfg, err := LoadStage(vp)
	require.NoError(t, err)
	assert.Equal(t, "/state/env/yacd-localnet-plan.json", cfg.PlanManifestFile, "explicit manifest path passes through")
}

func TestLoadStageRequiresFields(t *testing.T) {
	cases := map[string]string{
		"state-dir":                 "--state-dir",
		"output-dir":                "--output-dir",
		"cardano-network-name":      "--cardano-network-name",
		"cardano-network-namespace": "--cardano-network-namespace",
		"cardano-network-mode":      "--cardano-network-mode",
		"cardano-network-era":       "--cardano-network-era",
		"cardano-node-to-node-host": "--cardano-node-to-node-host",
	}
	for key, flag := range cases {
		t.Run(key, func(t *testing.T) {
			vp := baseStageViper(t)
			vp.Set(key, "")
			_, err := LoadStage(vp)
			require.Error(t, err)
			assert.Contains(t, err.Error(), flag)
		})
	}
}

func TestLoadStageRejectsBadPort(t *testing.T) {
	vp := baseStageViper(t)
	vp.Set("cardano-node-to-node-port", 0)
	_, err := LoadStage(vp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--cardano-node-to-node-port")
}

func TestLoadStageHonorsExplicitURL(t *testing.T) {
	vp := baseStageViper(t)
	vp.Set("cardano-node-to-node-url", "tcp://override:9999")
	cfg, err := LoadStage(vp)
	require.NoError(t, err)
	assert.Equal(t, "tcp://override:9999", cfg.CardanoNodeToNodeURL, "explicit url is not overwritten")
}
