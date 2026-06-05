package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAwaitConfirmationSucceedsWhenTxAppears(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"other", "abc123"}, nil)

	require.NoError(t, awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 5*time.Second))
}

func TestAwaitConfirmationMatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"ABC123"}, nil)

	require.NoError(t, awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 5*time.Second))
}

func TestAwaitConfirmationTimesOutWithoutMatch(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return([]string{"unrelated"}, nil)

	err := awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 50*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not confirmed within")
}

func TestAwaitConfirmationSurfacesLastQueryErrorOnTimeout(t *testing.T) {
	t.Parallel()

	confirmer := mocks.NewUTxOConfirmer(t)
	confirmer.EXPECT().TransactionIDs(mock.Anything, "addr_test1dest").Return(nil, errors.New("kupo unreachable"))

	err := awaitConfirmation(context.Background(), confirmer, "addr_test1dest", "abc123", 50*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kupo unreachable")
}
