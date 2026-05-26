// Package render synthesises Kubernetes manifests from a developer
// environment configuration.
//
// All functions in this package are pure: they perform no I/O, take no
// kube.Client, and return either a typed object or a marshalled byte slice.
// Namespace resolution follows a fixed precedence (override > configured >
// fallback > "default") that callers thread through the rendering pipeline.
package render
