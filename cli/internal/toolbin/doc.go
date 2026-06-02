// Package toolbin is the pinned-tool-binary resolver port for the YACD CLI.
//
// Resolver locates a verified, version-pinned external tool binary on demand,
// fetching it when it is not already present. The ghrelease subpackage is the
// implementation: it downloads a GitHub release asset, verifies it against a
// SHA256 embedded in the CLI at build time, installs it under an XDG data
// directory with the executable bit, and garbage-collects superseded versions.
// The embedded digest — not transport trust — is the integrity guarantee.
//
// This package stays dependency-light: it holds the Resolver interface, the Pin
// value that describes what to fetch and how to verify it, the small HTTPDoer
// seam the adapter depends on, and DefaultDir for XDG path resolution. The
// download, verification, and filesystem machinery live only in ghrelease, so
// consumers depend on the light port.
//
// The first (and currently only) tool is k3d, YACD's local end-user cluster
// runtime. P3's cluster/k3d adapter shells out to the resolved binary.
package toolbin
