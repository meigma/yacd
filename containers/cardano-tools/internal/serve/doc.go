// Package serve exposes an artifact directory over HTTP for out-of-cluster
// consumers (such as the developer CLI) that cannot mount a ConfigMap or PVC.
//
// Run serves a single resolved directory read-only with a default-deny
// allowlist: a request is served only when its path is exactly one of the
// known network artifact keys, checked both as requested and after symlink
// resolution. Anything else — traversal, directory listings, key material,
// backup files, symlinks that resolve to a non-artifact or outside the root —
// is refused by construction rather than by a denylist.
//
// The package exports Options and Run; everything else is unexported.
package serve
