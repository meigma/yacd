package ui_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/meigma/yacd/cli/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newIO(cfg ui.Config) (ui.IO, *bytes.Buffer, *bytes.Buffer) {
	var out, errw bytes.Buffer
	return ui.New(cfg, strings.NewReader(""), &out, &errw), &out, &errw
}

func TestEncodeMatchesMarshalIndent(t *testing.T) {
	value := map[string]any{"name": "devnet", "ready": true, "count": 3}
	io, out, errw := newIO(ui.Config{})

	require.NoError(t, io.Encode(value))

	expected, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	assert.Equal(t, string(expected)+"\n", out.String())
	assert.Empty(t, errw.String(), "Encode must not touch the human plane")
}

func TestEncodeEscapesHTMLLikeMarshalIndent(t *testing.T) {
	value := map[string]string{"url": "ws://host?a=1&b=2"}
	io, out, _ := newIO(ui.Config{})

	require.NoError(t, io.Encode(value))

	expected, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	// Byte-parity with MarshalIndent implies the same HTML escaping (& becomes
	// the & escape), so the frozen JSON shapes need no re-pinning.
	assert.Equal(t, string(expected)+"\n", out.String())
	assert.NotContains(t, out.String(), "&b=2", "the ampersand must be HTML-escaped, not literal")
}

func TestHumanHelpersWriteToErrPlane(t *testing.T) {
	io, out, errw := newIO(ui.Config{})

	io.Info("info\n")
	io.Status("status\n")
	io.Success("success\n")
	io.Detail("detail\n")
	io.Warn("warn\n")
	io.Error("error\n")

	assert.Empty(t, out.String(), "human helpers never write to stdout")
	got := errw.String()
	assert.Contains(t, got, "info\n")
	assert.Contains(t, got, "status\n")
	assert.Contains(t, got, "success\n")
	assert.Contains(t, got, "    detail\n")
	assert.Contains(t, got, "Warning: warn\n")
	assert.Contains(t, got, "error\n")
}

func TestHumanHelpersEmitNoEscapeByte(t *testing.T) {
	io, _, errw := newIO(ui.Config{Color: true})

	io.Info("info\n")
	io.Status("status\n")
	io.Success("success\n")
	io.Detail("detail\n")
	io.Warn("warn\n")
	io.Error("error\n")

	assert.NotContains(t, errw.String(), "\x1b", "the charm-free human plane must be ANSI-free")
}

func TestQuietMutesHumanPlaneButNotError(t *testing.T) {
	io, _, errw := newIO(ui.Config{Quiet: true})

	io.Info("info\n")
	io.Status("status\n")
	io.Success("success\n")
	io.Detail("detail\n")
	io.Warn("warn\n")
	assert.Empty(t, errw.String(), "Quiet mutes info/status/success/detail/warn")

	io.Error("error\n")
	assert.Equal(t, "error\n", errw.String(), "Quiet still prints errors")
}

func TestNewSlogLoggerDiscardsUnderQuiet(t *testing.T) {
	io, _, errw := newIO(ui.Config{Quiet: true})

	logger := io.NewSlogLogger(slog.LevelInfo, false)
	logger.Error("boom")

	assert.Empty(t, errw.String(), "Quiet forces the logger off")
}

func TestNewSlogLoggerWritesToErrPlane(t *testing.T) {
	io, out, errw := newIO(ui.Config{})

	logger := io.NewSlogLogger(slog.LevelInfo, false)
	logger.Info("hello")

	assert.Empty(t, out.String(), "logs never reach the data plane")
	assert.Contains(t, errw.String(), "hello")
}

func TestInteractiveFalseForBuffers(t *testing.T) {
	io, _, _ := newIO(ui.Config{})
	assert.False(t, io.Interactive(), "buffers are non-TTY so prompts are never allowed")
}

func TestAccessorsReflectConfig(t *testing.T) {
	io, _, _ := newIO(ui.Config{OutputJSON: true, Quiet: true, Color: true})
	assert.True(t, io.JSON())
	assert.True(t, io.Quiet())
	assert.True(t, io.Color())
	assert.False(t, io.ErrIsTTY(), "a buffer is never a terminal")
}

func TestReporterPlainWritesSteps(t *testing.T) {
	io, _, errw := newIO(ui.Config{})

	reporter := io.Reporter()
	reporter.Step("doing %s", "thing")
	reporter.Done("done")

	assert.Equal(t, "==> doing thing\n    done\n", errw.String())
}

func TestReporterNopUnderQuiet(t *testing.T) {
	io, _, errw := newIO(ui.Config{Quiet: true})

	reporter := io.Reporter()
	reporter.Step("doing thing")

	ran := false
	err := reporter.Run(context.Background(), "title", func(context.Context) error {
		ran = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, ran, "the no-op reporter still runs the action")
	assert.Empty(t, errw.String(), "Quiet reporter emits nothing")
}

func TestReporterRunRunsActionAndPropagatesError(t *testing.T) {
	io, _, _ := newIO(ui.Config{})
	sentinel := errors.New("boom")

	err := io.Reporter().Run(context.Background(), "title", func(context.Context) error {
		return sentinel
	})

	assert.ErrorIs(t, err, sentinel)
}

func TestConfigFromRuntime(t *testing.T) {
	view := ui.RuntimeView{OutputJSON: true, Quiet: true, Verbosity: 2, LogLevel: "debug", LogFormat: "json"}
	cfg := ui.ConfigFromRuntime(view, true, true)

	assert.Equal(t, ui.Config{
		OutputJSON:     true,
		Quiet:          true,
		NonInteractive: true,
		Color:          true,
		Verbosity:      2,
		LogLevel:       "debug",
		LogFormat:      "json",
	}, cfg)
}
