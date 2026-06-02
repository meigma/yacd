// Package ghrelease resolves a pinned tool binary from a GitHub release. It is
// the adapter behind the toolbin.Resolver port.
//
// Resolve fetches the per-platform asset described by a toolbin.Pin, verifies it
// against the embedded SHA256, installs it under the configured directory with
// the executable bit (atomically, via a temp file + rename), and removes
// superseded versions. GitHub release-asset URLs redirect to GitHub's download
// CDN, so the resolver follows redirects but only to GitHub download hosts and
// rejects any other host; the embedded digest is the integrity guarantee, so a
// tampered or wrong download fails closed regardless of where it came from.
//
// A pre-staged binary short-circuits the fetch: when YACD_K3D_PATH points at an
// existing file, Resolve returns it unverified, trusting the operator's choice.
//
// DefaultK3dPin (pin.go) is the built-in k3d pin; the composition root passes it
// plus toolbin.DefaultDir() into New. Tests inject a fake HTTPDoer and a temp
// directory.
package ghrelease
