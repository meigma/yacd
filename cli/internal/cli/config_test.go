package cli

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRaise(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		base    string
		verbose int
		want    string
	}{
		{"no verbose keeps base", "info", 0, "info"},
		{"info plus one is debug", "info", 1, "debug"},
		{"info caps at debug", "info", 5, "debug"},
		{"warn steps to info", "warn", 1, "info"},
		{"warn two steps to debug", "warn", 2, "debug"},
		{"error three steps to debug", "error", 3, "debug"},
		{"debug never lowers", "debug", 2, "debug"},
		{"warn unchanged without verbose", "warn", 0, "warn"},
		{"unknown base returned unchanged", "trace", 2, "trace"},
		{"negative verbose returned unchanged", "info", -1, "info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, raise(tc.base, tc.verbose))
		})
	}
}

func TestNewLoggerQuietForcesOff(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newLogger(RuntimeConfig{LogLevel: "debug"}, &buf, true)
	logger.Error("boom")

	assert.False(t, logger.Enabled(context.Background(), slog.LevelError),
		"quiet forces the logger off, overriding the level")
	assert.Empty(t, buf.String(), "a quiet logger writes nothing")
}

func TestNewLoggerRespectsLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newLogger(RuntimeConfig{LogLevel: "info"}, &buf, false)

	assert.True(t, logger.Enabled(context.Background(), slog.LevelInfo))
	assert.False(t, logger.Enabled(context.Background(), slog.LevelDebug),
		"the info level suppresses debug records")
}
