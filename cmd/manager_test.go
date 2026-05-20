package main

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewTLSOptions checks that newTLSOptions disables HTTP/2 by default and
// respects the EnableHTTP2 opt-in.
func TestNewTLSOptions(t *testing.T) {
	t.Run("disables HTTP/2 by default", func(t *testing.T) {
		tlsOpts := newTLSOptions(managerOptions{})
		require.Len(t, tlsOpts, 1)

		config := &tls.Config{NextProtos: []string{"h2"}}
		tlsOpts[0](config)

		assert.Equal(t, []string{"http/1.1"}, config.NextProtos)
	})

	t.Run("leaves HTTP/2 enabled when requested", func(t *testing.T) {
		tlsOpts := newTLSOptions(managerOptions{EnableHTTP2: true})

		assert.Empty(t, tlsOpts)
	})
}

// TestNewWebhookServerOptions verifies that explicit webhook certificate
// flags are propagated into the controller-runtime webhook options.
func TestNewWebhookServerOptions(t *testing.T) {
	tlsOpts := []func(*tls.Config){func(*tls.Config) {}}
	options := managerOptions{
		WebhookCertPath: "/webhook-certs",
		WebhookCertName: "webhook.crt",
		WebhookCertKey:  "webhook.key",
	}

	got := newWebhookServerOptions(options, tlsOpts)

	require.Len(t, got.TLSOpts, 1)
	assert.Equal(t, "/webhook-certs", got.CertDir)
	assert.Equal(t, "webhook.crt", got.CertName)
	assert.Equal(t, "webhook.key", got.KeyName)
}

// TestNewMetricsServerOptions covers both secure and insecure metrics server
// configurations, confirming that authn/authz filtering is only applied when
// MetricsSecure is set.
func TestNewMetricsServerOptions(t *testing.T) {
	tlsOpts := []func(*tls.Config){func(*tls.Config) {}}

	t.Run("configures secure metrics", func(t *testing.T) {
		options := managerOptions{
			MetricsBindAddress: ":8443",
			MetricsSecure:      true,
			MetricsCertPath:    "/metrics-certs",
			MetricsCertName:    "metrics.crt",
			MetricsCertKey:     "metrics.key",
		}

		got := newMetricsServerOptions(options, tlsOpts)

		assert.Equal(t, ":8443", got.BindAddress)
		assert.True(t, got.SecureServing)
		assert.NotNil(t, got.FilterProvider)
		require.Len(t, got.TLSOpts, 1)
		assert.Equal(t, "/metrics-certs", got.CertDir)
		assert.Equal(t, "metrics.crt", got.CertName)
		assert.Equal(t, "metrics.key", got.KeyName)
	})

	t.Run("leaves insecure metrics unauthenticated", func(t *testing.T) {
		options := managerOptions{
			MetricsBindAddress: ":8080",
			MetricsSecure:      false,
		}

		got := newMetricsServerOptions(options, tlsOpts)

		assert.Equal(t, ":8080", got.BindAddress)
		assert.False(t, got.SecureServing)
		assert.Nil(t, got.FilterProvider)
		require.Len(t, got.TLSOpts, 1)
	})
}

// TestNewManagerOptions exercises newManagerOptions end to end, confirming
// that command-line flags propagate into the controller-runtime Options that
// drive manager construction.
func TestNewManagerOptions(t *testing.T) {
	options, err := parseManagerOptions([]string{
		"--health-probe-bind-address=:9091",
		"--leader-elect",
		"--metrics-bind-address=:8443",
	})
	require.NoError(t, err)

	got := newManagerOptions(options)

	assert.Same(t, scheme, got.Scheme)
	assert.Equal(t, ":9091", got.HealthProbeBindAddress)
	assert.True(t, got.LeaderElection)
	assert.Equal(t, leaderElectionID, got.LeaderElectionID)
	assert.Equal(t, ":8443", got.Metrics.BindAddress)
	assert.NotNil(t, got.WebhookServer)
}
