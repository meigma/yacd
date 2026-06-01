// Package artifactsync mirrors a served Cardano network artifact bundle from an
// HTTP serve endpoint into a local output directory, verifying every file
// against the served integrity manifest.
//
// It implements the yacd-cardano-tools "sync" command, the consumer half of the
// F0 transport redesign. The CardanoNetwork primary pod stages a flat artifact
// directory onto its node PVC and exposes it over HTTP through an always-on
// serve sidecar, publishing the endpoint URL in status. A follower (db-sync)
// pod cannot mount that PVC cross-pod, so its init container runs this command
// to pull the bundle over HTTP instead.
//
// Run reads manifest.json from the serve endpoint, then fetches each file the
// manifest names and verifies its sha256 digest before writing it. It fails
// closed on a non-200 response, a redirect, a digest mismatch, a missing file,
// or an oversize body, so a tampered or incomplete bundle never reaches the
// follower node. The fetched manifest.json is written verbatim last, so the
// output directory is self-describing and a re-run is idempotent.
//
// The package exports Options and Run; the HTTP seam is unexported.
package artifactsync
