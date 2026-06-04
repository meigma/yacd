package operator

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
)

// TestDefaultPinsReleaseDigests proves the offline default install stays
// digest-pinned to the published release on both the manager and faucet images.
func TestDefaultPinsReleaseDigests(t *testing.T) {
	values := Default()

	assert.Equal(t, defaultManagerRepository, values.Image.Repository)
	assert.Equal(t, defaultManagerDigest, values.Image.Digest)
	assert.Empty(t, values.Image.Tag, "digest pin leaves tag empty")

	assert.Equal(t, defaultFaucetRepository, values.FaucetImage.Repository)
	assert.Equal(t, defaultFaucetDigest, values.FaucetImage.Digest)
	assert.Empty(t, values.FaucetImage.Tag, "digest pin leaves tag empty")

	helmValues := values.ToHelmValues()
	image, ok := helmValues["image"].(map[string]any)
	require.True(t, ok, "image sub-tree must be present")
	assert.Equal(t, defaultManagerDigest, image["digest"])
	_, hasTag := image["tag"]
	assert.False(t, hasTag, "empty tag is omitted so the chart default applies")

	faucet, ok := helmValues["faucet"].(map[string]any)
	require.True(t, ok)
	faucetImage, ok := faucet["image"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, defaultFaucetDigest, faucetImage["digest"])
}

// TestToHelmValuesMergesExtraOverTypedFields proves Extra is deep-merged last so
// it overrides typed fields (including nested ones) while preserving sibling
// keys the typed fields set.
func TestToHelmValuesMergesExtraOverTypedFields(t *testing.T) {
	replicas := 3
	values := Values{
		Image: Image{Repository: "example.com/mgr", Digest: "sha256:aaa"},
		FaucetImage: Image{
			Repository: "example.com/faucet",
			Digest:     "sha256:bbb",
		},
		Replicas:  &replicas,
		LogFormat: "json",
		LogLevel:  "info",
		Extra: map[string]any{
			// Override a nested typed field (image.digest) while keeping the
			// sibling repository the typed field set.
			"image": map[string]any{"digest": "sha256:override"},
			// Override a scalar typed field.
			"replicaCount": 7,
			// Set a knob the typed contract does not model.
			"nameOverride": "custom",
		},
	}

	got := values.ToHelmValues()

	image, ok := got["image"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sha256:override", image["digest"], "Extra wins on conflict")
	assert.Equal(t, "example.com/mgr", image["repository"], "sibling typed field is preserved through the deep merge")

	assert.Equal(t, 7, got["replicaCount"], "Extra scalar overrides the typed field")
	assert.Equal(t, "custom", got["nameOverride"], "Extra can set unmodeled knobs")

	manager, ok := got["manager"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json", manager["logFormat"])
	assert.Equal(t, "info", manager["logLevel"])

	faucet, ok := got["faucet"].(map[string]any)
	require.True(t, ok)
	faucetImage, ok := faucet["image"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sha256:bbb", faucetImage["digest"], "untouched typed fields survive the merge")
}

// TestZeroValuesFallThroughToChartDefaults proves an unset typed field is
// omitted from the Helm values tree, so the chart's own values.yaml default
// applies rather than a Go zero value clobbering it.
func TestZeroValuesFallThroughToChartDefaults(t *testing.T) {
	got := Values{}.ToHelmValues()

	assert.NotContains(t, got, "replicaCount", "nil Replicas is omitted")
	assert.NotContains(t, got, "manager", "empty log knobs are omitted")
	assert.NotContains(t, got, "leaderElection", "nil LeaderElection is omitted")
	assert.NotContains(t, got, "image", "empty Image is omitted")
}

// TestDefaultValuesValidateAgainstChartSchema proves the typed Default() values,
// once coalesced with the chart's own defaults, satisfy
// charts/yacd/values.schema.json. This is the contract that the typed install
// surface never produces a values tree the packaged chart would reject.
func TestDefaultValuesValidateAgainstChartSchema(t *testing.T) {
	ch, err := loader.Load(chartDir(t))
	require.NoError(t, err, "load source chart")
	require.NotNil(t, ch.Schema, "chart must ship values.schema.json")

	coalesced, err := chartutil.CoalesceValues(ch, Default().ToHelmValues())
	require.NoError(t, err, "coalesce typed defaults over chart defaults")

	err = chartutil.ValidateAgainstSchema(ch, coalesced)
	require.NoError(t, err, "coalesced default values must satisfy the chart schema")
}

// chartDir resolves the source chart directory relative to this test file so the
// lookup is robust regardless of the working directory the test runs from.
func chartDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolve caller for chart path")
	// this file: <repo>/cli/internal/operator/values_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "charts", "yacd")
}
