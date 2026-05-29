package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		arg           string
		namespaceFlag string
		wantName      string
		wantNamespace string
		wantErr       string
	}{
		{
			name:          "namespace defaults to name",
			arg:           "devnet",
			wantName:      "devnet",
			wantNamespace: "devnet",
		},
		{
			name:          "trims surrounding whitespace",
			arg:           "  devnet  ",
			wantName:      "devnet",
			wantNamespace: "devnet",
		},
		{
			name:          "namespace flag overrides default",
			arg:           "devnet",
			namespaceFlag: "team-a",
			wantName:      "devnet",
			wantNamespace: "team-a",
		},
		{
			name:          "namespace flag is trimmed",
			arg:           "devnet",
			namespaceFlag: "  team-a  ",
			wantName:      "devnet",
			wantNamespace: "team-a",
		},
		{
			name:    "empty name rejected",
			arg:     "",
			wantErr: "NAME is required",
		},
		{
			name:    "blank name rejected",
			arg:     "   ",
			wantErr: "NAME is required",
		},
		{
			name:    "uppercase name rejected",
			arg:     "Devnet",
			wantErr: "invalid NAME",
		},
		{
			name:    "underscore name rejected",
			arg:     "dev_net",
			wantErr: "invalid NAME",
		},
		{
			name:          "invalid namespace flag rejected",
			arg:           "devnet",
			namespaceFlag: "Team_A",
			wantErr:       "invalid namespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotName, gotNamespace, err := resolveIdentity(tc.arg, RuntimeConfig{Namespace: tc.namespaceFlag})
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, gotName)
			assert.Equal(t, tc.wantNamespace, gotNamespace)
		})
	}
}
