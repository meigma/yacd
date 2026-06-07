package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meigma/yacd/cli/internal/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNopReporterRun(t *testing.T) {
	t.Run("runs the action with the given context", func(t *testing.T) {
		ctx := context.Background()
		var gotCtx context.Context
		called := false

		err := lifecycle.NopReporter{}.Run(ctx, "any title", func(actionCtx context.Context) error {
			called = true
			gotCtx = actionCtx
			return nil
		})

		require.NoError(t, err)
		assert.True(t, called, "action must run")
		assert.Equal(t, ctx, gotCtx, "action must receive the passed context")
	})

	t.Run("propagates the action error", func(t *testing.T) {
		sentinel := errors.New("boom")

		err := lifecycle.NopReporter{}.Run(context.Background(), "any title", func(context.Context) error {
			return sentinel
		})

		assert.ErrorIs(t, err, sentinel)
	})
}
