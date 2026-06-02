package operator_test

import (
	"testing"

	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		embedded   string
		state      operator.State
		wantAction operator.Action
		wantErr    error
	}{
		{
			name:       "absent installs",
			embedded:   "v0.1.1",
			state:      operator.State{Installed: false},
			wantAction: operator.ActionInstall,
		},
		{
			name:       "equal version is a noop reapply",
			embedded:   "v0.1.1",
			state:      operator.State{Installed: true, Version: "v0.1.1"},
			wantAction: operator.ActionNoop,
		},
		{
			name:       "older same-major upgrades",
			embedded:   "v0.2.0",
			state:      operator.State{Installed: true, Version: "v0.1.1"},
			wantAction: operator.ActionUpgrade,
		},
		{
			name:       "newer same-major refuses",
			embedded:   "v0.1.1",
			state:      operator.State{Installed: true, Version: "v0.2.0"},
			wantAction: operator.ActionRefuse,
			wantErr:    operator.ErrNewerOperator,
		},
		{
			name:       "lower major refuses as mismatch",
			embedded:   "v2.0.0",
			state:      operator.State{Installed: true, Version: "v1.9.0"},
			wantAction: operator.ActionRefuse,
			wantErr:    operator.ErrMajorMismatch,
		},
		{
			name:       "higher installed major refuses as mismatch before newer check",
			embedded:   "v1.0.0",
			state:      operator.State{Installed: true, Version: "v2.0.0"},
			wantAction: operator.ActionRefuse,
			wantErr:    operator.ErrMajorMismatch,
		},
		{
			name:       "installed without a valid version refuses",
			embedded:   "v0.1.1",
			state:      operator.State{Installed: true, Version: ""},
			wantAction: operator.ActionRefuse,
			wantErr:    operator.ErrUnknownInstalledVersion,
		},
		{
			name:       "invalid embedded version refuses",
			embedded:   "0.1.1",
			state:      operator.State{Installed: false},
			wantAction: operator.ActionRefuse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := operator.Decide(tt.embedded, tt.state)
			assert.Equal(t, tt.wantAction, action)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			if tt.wantAction == operator.ActionRefuse {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
