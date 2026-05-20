package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseManagerOptions covers the manager flag parser across defaults,
// the manifest argument shape, negatable booleans, and the slog logging
// switches.
func TestParseManagerOptions(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		assert func(t *testing.T, options managerOptions)
	}{
		{
			name: "uses operator defaults",
			assert: func(t *testing.T, options managerOptions) {
				assert.Equal(t, "0", options.MetricsBindAddress)
				assert.Equal(t, ":8081", options.HealthProbeBindAddress)
				assert.False(t, options.LeaderElect)
				assert.True(t, options.MetricsSecure)
				assert.Equal(t, "tls.crt", options.WebhookCertName)
				assert.Equal(t, "tls.key", options.WebhookCertKey)
				assert.Equal(t, "tls.crt", options.MetricsCertName)
				assert.Equal(t, "tls.key", options.MetricsCertKey)
				assert.False(t, options.EnableHTTP2)
				assert.Equal(t, "json", options.LogFormat)
				assert.Equal(t, "info", options.LogLevel)
			},
		},
		{
			name: "accepts current manifest args",
			args: []string{
				"--metrics-bind-address=:8443",
				"--leader-elect",
				"--health-probe-bind-address=:8081",
			},
			assert: func(t *testing.T, options managerOptions) {
				assert.Equal(t, ":8443", options.MetricsBindAddress)
				assert.Equal(t, ":8081", options.HealthProbeBindAddress)
				assert.True(t, options.LeaderElect)
			},
		},
		{
			name: "disables secure metrics with explicit false",
			args: []string{"--metrics-secure=false"},
			assert: func(t *testing.T, options managerOptions) {
				assert.False(t, options.MetricsSecure)
			},
		},
		{
			name: "disables secure metrics with negated bool",
			args: []string{"--no-metrics-secure"},
			assert: func(t *testing.T, options managerOptions) {
				assert.False(t, options.MetricsSecure)
			},
		},
		{
			name: "accepts slog logging options",
			args: []string{"--log-format=text", "--log-level=debug"},
			assert: func(t *testing.T, options managerOptions) {
				assert.Equal(t, "text", options.LogFormat)
				assert.Equal(t, "debug", options.LogLevel)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := parseManagerOptions(tt.args)
			require.NoError(t, err)
			tt.assert(t, options)
		})
	}
}

// TestNewManagerParserUsesYACDName keeps release binary help aligned with the
// published artifact name.
func TestNewManagerParserUsesYACDName(t *testing.T) {
	parser, err := newManagerParser(&managerOptions{})
	require.NoError(t, err)

	assert.Equal(t, "yacd", parser.Model.Name)
}

// TestParseManagerOptionsRejectsZapFlags asserts the parser refuses legacy
// zap flags so operators do not silently lose configuration after the slog
// migration.
func TestParseManagerOptionsRejectsZapFlags(t *testing.T) {
	_, err := parseManagerOptions([]string{"--zap-devel=true"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zap-devel")
}

// TestParseManagerOptionsRejectsInvalidLogOptions checks that unsupported log
// format and level values surface a parser error rather than silently
// defaulting to info/json.
func TestParseManagerOptionsRejectsInvalidLogOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "format",
			args: []string{"--log-format=console"},
		},
		{
			name: "level",
			args: []string{"--log-level=trace"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseManagerOptions(tt.args)
			require.Error(t, err)
		})
	}
}

// TestNewControllerLogger exercises every supported format/level combination
// to make sure newControllerLogger produces a working logger for each.
func TestNewControllerLogger(t *testing.T) {
	formats := []string{"json", "text"}
	levels := []string{"debug", "info", "warn", "error"}

	for _, format := range formats {
		for _, level := range levels {
			t.Run(format+"_"+level, func(t *testing.T) {
				var out bytes.Buffer
				logger, err := newControllerLogger(managerOptions{
					LogFormat: format,
					LogLevel:  level,
				}, &out)
				require.NoError(t, err)

				logger.Error(errors.New("boom"), "logger constructed", "level", level)

				assert.Contains(t, out.String(), "logger constructed")
			})
		}
	}
}

// TestNewControllerLoggerRejectsInvalidOptions asserts that newControllerLogger
// returns an error rather than falling back to a default handler when the
// caller supplies an unknown format or level.
func TestNewControllerLoggerRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name    string
		options managerOptions
	}{
		{
			name: "format",
			options: managerOptions{
				LogFormat: "console",
				LogLevel:  "info",
			},
		},
		{
			name: "level",
			options: managerOptions{
				LogFormat: "json",
				LogLevel:  "trace",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newControllerLogger(tt.options, &bytes.Buffer{})
			require.Error(t, err)
		})
	}
}
