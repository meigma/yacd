// Package kube is the Kubernetes seam the report verb uses to publish an
// artifact set into the network artifact ConfigMap.
//
// Client is the port the command layer consumes; Adapter is the
// client-go-backed implementation. NewClient returns the concrete *Adapter so
// callers own its lifecycle and pass the Client interface to the commands they
// construct. The merge-patch rendering is exported through
// MarshalMergePatchIndented for dry-run output.
//
// The package exports Client, ConfigMapPatch, Config, Adapter, NewClient, and
// MarshalMergePatchIndented; everything else is unexported.
package kube
