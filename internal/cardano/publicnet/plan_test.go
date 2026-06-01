package publicnet

import (
	"strings"
	"testing"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlanCuratedProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		profile               string
		wantNetworkMagic      int64
		wantRequiresMagic     bool
		wantSource            string
		wantCompatibleRelease string
		wantOptionalArtifacts []string
		wantMissingArtifacts  []string
		wantPinnedFingerprint string
		bootstrap             *BootstrapSpec
	}{
		{
			profile:               previewProfileName,
			wantNetworkMagic:      2,
			wantRequiresMagic:     true,
			wantSource:            "https://book.play.dev.cardano.org/environments/preview/",
			wantCompatibleRelease: operationsBookNodeRelease,
			wantOptionalArtifacts: []string{
				networkartifacts.CheckpointsKey,
				networkartifacts.PeerSnapshotKey,
			},
			wantPinnedFingerprint: "3eee469d6200db89fd64fbd032ccbb58a7ba557b920a07bc2f22523b6f009a29",
		},
		{
			profile:               preprodProfileName,
			wantNetworkMagic:      1,
			wantRequiresMagic:     true,
			wantSource:            "https://book.play.dev.cardano.org/environments/preprod/",
			wantCompatibleRelease: operationsBookNodeRelease,
			wantOptionalArtifacts: []string{
				networkartifacts.PeerSnapshotKey,
			},
			wantMissingArtifacts: []string{
				networkartifacts.CheckpointsKey,
			},
		},
		{
			profile:               mainnetProfileName,
			wantNetworkMagic:      764824073,
			wantRequiresMagic:     false,
			wantSource:            "https://book.play.dev.cardano.org/environments/mainnet/",
			wantCompatibleRelease: operationsBookNodeRelease,
			wantOptionalArtifacts: []string{
				networkartifacts.CheckpointsKey,
				networkartifacts.PeerSnapshotKey,
				networkartifacts.MithrilGenesisKey,
				networkartifacts.MithrilAncillaryKey,
			},
			bootstrap: &BootstrapSpec{Mithril: &MithrilBootstrapSpec{}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()

			plan, err := BuildPlan(Spec{
				Profile:   tc.profile,
				Bootstrap: tc.bootstrap,
				Paths:     Paths{ProfileDir: "/profile"},
			})
			require.NoError(t, err)

			assert.Equal(t, tc.profile, plan.Profile)
			assert.Equal(t, tc.wantNetworkMagic, plan.NetworkMagic)
			assert.Equal(t, tc.wantRequiresMagic, plan.RequiresNetworkMagic)
			assert.Equal(t, "/profile/configuration.yaml", plan.Layout.ConfigFile)
			assert.Equal(t, "/profile/primary-topology.json", plan.Layout.TopologyFile)
			assert.Equal(t, "sha256", plan.Fingerprint.Algorithm)
			assert.NotEmpty(t, plan.Fingerprint.Value)
			if tc.wantPinnedFingerprint != "" {
				assert.Equal(t, tc.wantPinnedFingerprint, plan.Fingerprint.Value)
			}
			assert.Equal(t, plan.Fingerprint, plan.Manifest.Fingerprint)
			assert.Equal(t, "yacd.meigma.io/public-network-profile/v1alpha1", plan.Manifest.SchemaVersion)
			assert.Equal(t, tc.wantSource, plan.Manifest.Source)
			assert.Equal(t, tc.wantCompatibleRelease, plan.Manifest.CompatibleNodeRelease)

			for _, key := range []string{
				networkartifacts.ConfigurationKey,
				networkartifacts.ByronGenesisKey,
				networkartifacts.ShelleyGenesisKey,
				networkartifacts.AlonzoGenesisKey,
				networkartifacts.ConwayGenesisKey,
				networkartifacts.PrimaryTopologyKey,
				networkartifacts.PublicProfileManifestKey,
			} {
				assert.NotEmpty(t, plan.Artifacts[key], "artifact %s", key)
			}
			for _, key := range tc.wantOptionalArtifacts {
				assert.NotEmpty(t, plan.Artifacts[key], "artifact %s", key)
			}
			for _, key := range tc.wantMissingArtifacts {
				assert.Empty(t, plan.Artifacts[key], "artifact %s", key)
			}
			if tc.profile == mainnetProfileName {
				require.NotNil(t, plan.Mithril)
				assert.Equal(t, defaultMithrilClientImage, plan.Mithril.Image)
				assert.Equal(t, defaultMithrilSnapshot, plan.Mithril.Snapshot)
				assert.Equal(t, releaseMainnetMithrilAggregator, plan.Mithril.AggregatorEndpoint)
				assert.Equal(t, strings.TrimSpace(plan.Artifacts[networkartifacts.MithrilGenesisKey]), plan.Mithril.GenesisVerificationKey)
				assert.Equal(t, strings.TrimSpace(plan.Artifacts[networkartifacts.MithrilAncillaryKey]), plan.Mithril.AncillaryVerificationKey)
			} else {
				assert.Nil(t, plan.Mithril)
			}
		})
	}
}

func TestBuildPlanFingerprintIsStableAcrossMountDirs(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{previewProfileName, preprodProfileName, mainnetProfileName} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()

			spec := Spec{Profile: profile, Paths: Paths{ProfileDir: "/profile"}}
			if profile == mainnetProfileName {
				spec.Bootstrap = &BootstrapSpec{Mithril: &MithrilBootstrapSpec{}}
			}
			first, err := BuildPlan(spec)
			require.NoError(t, err)
			spec.Paths.ProfileDir = "/other"
			second, err := BuildPlan(spec)
			require.NoError(t, err)

			assert.Equal(t, first.Fingerprint, second.Fingerprint)
			assert.Equal(t, first.NetworkMagic, second.NetworkMagic)
		})
	}
}

func TestBuildPlanNormalizesMainnetMithrilBootstrap(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan(Spec{
		Profile: mainnetProfileName,
		Bootstrap: &BootstrapSpec{
			Mithril: &MithrilBootstrapSpec{
				Image:    "example.com/mithril-client:test",
				Snapshot: "abcdef",
			},
		},
	})
	require.NoError(t, err)

	require.NotNil(t, plan.Mithril)
	assert.Equal(t, "example.com/mithril-client:test", plan.Mithril.Image)
	assert.Equal(t, "abcdef", plan.Mithril.Snapshot)
}

func TestBuildPlanRejectsUnsupportedProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    Spec
		wantErr string
	}{
		{
			name:    "missing profile",
			spec:    Spec{},
			wantErr: "public profile is required",
		},
		{
			name:    "unknown profile",
			spec:    Spec{Profile: "unknown"},
			wantErr: `public profile "unknown" is not supported`,
		},
		{
			name:    "mainnet without mithril bootstrap",
			spec:    Spec{Profile: mainnetProfileName},
			wantErr: "public mainnet profile requires mithril bootstrap",
		},
		{
			name: "preview with mithril bootstrap",
			spec: Spec{
				Profile:   previewProfileName,
				Bootstrap: &BootstrapSpec{Mithril: &MithrilBootstrapSpec{}},
			},
			wantErr: "public bootstrap is supported only for mainnet",
		},
		{
			name: "preprod with mithril bootstrap",
			spec: Spec{
				Profile:   preprodProfileName,
				Bootstrap: &BootstrapSpec{Mithril: &MithrilBootstrapSpec{}},
			},
			wantErr: "public bootstrap is supported only for mainnet",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildPlan(tc.spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
