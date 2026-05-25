package apply

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnsupported(t *testing.T) {
	err := Unsupported("ResourceConflict", "resource %s is unavailable", "testing/child")

	assert.Equal(t, "ResourceConflict", err.Reason)
	assert.Equal(t, "resource testing/child is unavailable", err.Message)
	assert.Equal(t, err.Message, err.Error())
}

func TestUnsupportedErrorSupportsErrorsAs(t *testing.T) {
	err := error(Unsupported("UnsupportedSpec", "bad spec"))

	var unsupported UnsupportedError
	require.True(t, errors.As(err, &unsupported))
	assert.Equal(t, "UnsupportedSpec", unsupported.Reason)
	assert.Equal(t, "bad spec", unsupported.Message)
}
