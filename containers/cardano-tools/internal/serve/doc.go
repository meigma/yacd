// Package serve exposes an artifact directory over HTTP for out-of-cluster
// consumers (such as the developer CLI) that cannot mount a ConfigMap or PVC.
//
// Run serves a single resolved directory read-only. It is hardened against
// directory traversal three ways: os.DirFS-style cleaning rejects ".." and
// absolute escapes, every request path component is checked against
// artifactset.IsSecretComponent so key material is never served even when the
// directory contains it, and symlinks that resolve outside the served root are
// refused. Directory listings are disabled.
//
// The package exports Options and Run; everything else is unexported.
package serve
