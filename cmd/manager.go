package main

import (
	"crypto/tls"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const leaderElectionID = "8719486c.meigma.io"

// mustNewManager builds a controller-runtime manager from the given options
// and exits the process if construction fails.
func mustNewManager(options managerOptions) manager.Manager {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), newManagerOptions(options))
	exitOnError(err, "Failed to start manager")

	return mgr
}

// newManagerOptions translates parsed managerOptions into the controller-runtime
// Options used by ctrl.NewManager, including metrics, webhook, and leader
// election wiring.
func newManagerOptions(options managerOptions) ctrl.Options {
	tlsOpts := newTLSOptions(options)

	return ctrl.Options{
		Scheme:                 scheme,
		Metrics:                newMetricsServerOptions(options, tlsOpts),
		WebhookServer:          newWebhookServer(options, tlsOpts),
		HealthProbeBindAddress: options.HealthProbeBindAddress,
		LeaderElection:         options.LeaderElect,
		LeaderElectionID:       leaderElectionID,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	}
}

// newTLSOptions returns the TLS mutators applied to webhook and metrics
// servers. HTTP/2 is disabled by default to avoid CVE-2023-44487 and
// CVE-2023-39325 unless the caller opts in via --enable-http2.
func newTLSOptions(options managerOptions) []func(*tls.Config) {
	if options.EnableHTTP2 {
		return nil
	}

	return []func(*tls.Config){disableHTTP2}
}

// disableHTTP2 strips HTTP/2 from the supplied TLS config by advertising only
// http/1.1 via ALPN.
func disableHTTP2(c *tls.Config) {
	setupLog.Info("Disabling HTTP/2")
	c.NextProtos = []string{"http/1.1"}
}

// newWebhookServer constructs the controller-runtime webhook server using the
// options derived from the manager command-line flags.
func newWebhookServer(options managerOptions, tlsOpts []func(*tls.Config)) webhook.Server {
	return webhook.NewServer(newWebhookServerOptions(options, tlsOpts))
}

// newWebhookServerOptions builds the webhook.Options struct, attaching the
// supplied TLS mutators and any explicit certificate locations the operator
// is configured with.
func newWebhookServerOptions(options managerOptions, tlsOpts []func(*tls.Config)) webhook.Options {
	serverOptions := webhook.Options{
		TLSOpts: tlsOpts,
	}

	if len(options.WebhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", options.WebhookCertPath,
			"webhook-cert-name", options.WebhookCertName,
			"webhook-cert-key", options.WebhookCertKey)

		serverOptions.CertDir = options.WebhookCertPath
		serverOptions.CertName = options.WebhookCertName
		serverOptions.KeyName = options.WebhookCertKey
	}

	return serverOptions
}

// newMetricsServerOptions builds the controller-runtime metrics server
// options, enabling Kubernetes authn/authz filtering when secure serving is
// requested and attaching explicit certificate material when configured.
func newMetricsServerOptions(options managerOptions, tlsOpts []func(*tls.Config)) metricsserver.Options {
	serverOptions := metricsserver.Options{
		BindAddress:   options.MetricsBindAddress,
		SecureServing: options.MetricsSecure,
		TLSOpts:       tlsOpts,
	}

	if options.MetricsSecure {
		// FilterProvider protects metrics with Kubernetes authn/authz.
		serverOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(options.MetricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", options.MetricsCertPath,
			"metrics-cert-name", options.MetricsCertName,
			"metrics-cert-key", options.MetricsCertKey)

		serverOptions.CertDir = options.MetricsCertPath
		serverOptions.CertName = options.MetricsCertName
		serverOptions.KeyName = options.MetricsCertKey
	}

	return serverOptions
}
