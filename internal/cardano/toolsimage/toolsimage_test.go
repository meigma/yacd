package toolsimage_test

import (
	"testing"

	"github.com/meigma/yacd/internal/cardano/toolsimage"
	"github.com/stretchr/testify/assert"
)

func TestReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		override    string
		toolVersion string
		want        string
	}{
		{
			name:        "built-in reference when override empty",
			override:    "",
			toolVersion: "11.0.1",
			want:        "ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0",
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
			want:        "ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0",
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
