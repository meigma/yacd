package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantCode  int
		wantPrint bool
	}{
		{
			name:      "nil error exits zero and prints nothing",
			err:       nil,
			wantCode:  0,
			wantPrint: false,
		},
		{
			name:      "ordinary error exits one and is printed",
			err:       errors.New("boom"),
			wantCode:  1,
			wantPrint: true,
		},
		{
			name:      "silent exit error carries its code without printing",
			err:       newExitError(2, ""),
			wantCode:  2,
			wantPrint: false,
		},
		{
			name:      "exit error with a message is printed",
			err:       newExitError(3, "lost connection to the network"),
			wantCode:  3,
			wantPrint: true,
		},
		{
			name:      "wrapped exit error is still resolved",
			err:       fmt.Errorf("run go test: %w", newExitError(130, "")),
			wantCode:  130,
			wantPrint: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, printErr := ResolveExit(tt.err)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantPrint, printErr)
		})
	}
}

func TestExitErrorMessage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "exit status 7", newExitError(7, "").Error())
	assert.Equal(t, "custom message", newExitError(7, "custom message").Error())
}
