package publicnet

import (
	"testing"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlanPreview(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan(Spec{
		Profile: "preview",
		Paths:   Paths{ProfileDir: "/profile"},
	})
	require.NoError(t, err)

	assert.Equal(t, "preview", plan.Profile)
	assert.Equal(t, int64(2), plan.NetworkMagic)
	assert.True(t, plan.RequiresNetworkMagic)
	assert.Equal(t, "/profile/configuration.yaml", plan.Layout.ConfigFile)
	assert.Equal(t, "/profile/primary-topology.json", plan.Layout.TopologyFile)
	assert.Equal(t, "sha256", plan.Fingerprint.Algorithm)
	assert.Equal(t, "3eee469d6200db89fd64fbd032ccbb58a7ba557b920a07bc2f22523b6f009a29", plan.Fingerprint.Value)
	assert.Equal(t, plan.Fingerprint, plan.Manifest.Fingerprint)
	assert.Equal(t, "yacd.meigma.io/public-network-profile/v1alpha1", plan.Manifest.SchemaVersion)

	for _, key := range []string{
		networkartifacts.ConfigurationKey,
		networkartifacts.ByronGenesisKey,
		networkartifacts.ShelleyGenesisKey,
		networkartifacts.AlonzoGenesisKey,
		networkartifacts.ConwayGenesisKey,
		networkartifacts.PrimaryTopologyKey,
		networkartifacts.CheckpointsKey,
		networkartifacts.PeerSnapshotKey,
		networkartifacts.PublicProfileManifestKey,
	} {
		assert.NotEmpty(t, plan.Artifacts[key], "artifact %s", key)
	}
}

func TestBuildPlanPreviewFingerprintIsStable(t *testing.T) {
	t.Parallel()

	first, err := BuildPlan(Spec{Profile: "preview", Paths: Paths{ProfileDir: "/profile"}})
	require.NoError(t, err)
	second, err := BuildPlan(Spec{Profile: "preview", Paths: Paths{ProfileDir: "/other"}})
	require.NoError(t, err)

	assert.Equal(t, first.Fingerprint, second.Fingerprint)
	assert.Equal(t, first.NetworkMagic, second.NetworkMagic)
}

func TestBuildPlanRejectsUnsupportedProfiles(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"", "preprod", "mainnet", "custom"} {
		t.Run(profile, func(t *testing.T) {
			_, err := BuildPlan(Spec{Profile: profile})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "public profile")
		})
	}
}
