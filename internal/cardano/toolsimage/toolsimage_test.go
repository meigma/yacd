package toolsimage_test

import (
	"strings"
	"testing"

	"github.com/meigma/yacd/internal/cardano/toolsimage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReference(t *testing.T) {
	t.Parallel()

	builtIn := "ghcr.io/meigma/yacd/cardano-tools:11.0.1-" + toolsimage.Revision + "@" + toolsimage.Digest

	tests := []struct {
		name        string
		override    string
		toolVersion string
		want        string
	}{
		{
			name:        "built-in reference is digest-pinned when override empty",
			override:    "",
			toolVersion: "11.0.1",
			want:        builtIn,
		},
		{
			name:        "override wins",
			override:    "ghcr.io/meigma/yacd/cardano-tools:tilt",
			toolVersion: "11.0.1",
			want:        "ghcr.io/meigma/yacd/cardano-tools:tilt",
		},
		{
			name:        "whitespace-only override is ignored",
			override:    "   ",
			toolVersion: "11.0.1",
			want:        builtIn,
		},
		{
			name:        "override is trimmed",
			override:    "  ghcr.io/meigma/yacd/cardano-tools@sha256:abc  ",
			toolVersion: "11.0.1",
			want:        "ghcr.io/meigma/yacd/cardano-tools@sha256:abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, toolsimage.Reference(tt.override, tt.toolVersion))
		})
	}
}

// TestDigestPin guards the production invariant that the no-override default is
// pinned to a sha256 digest, so a stock install can never resolve a mutable tag.
func TestDigestPin(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, toolsimage.Digest, "built-in reference must carry a published digest")
	assert.True(t, strings.HasPrefix(toolsimage.Digest, "sha256:"), "Digest must be a sha256 reference")

	ref := toolsimage.Reference("", "11.0.1")
	assert.Contains(t, ref, "@"+toolsimage.Digest, "default reference must be digest-pinned")
}
