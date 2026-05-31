package networkartifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// ManifestKey is the data key and served filename of the artifact integrity
// manifest. The init container that stages the network artifacts onto the node
// state PVC writes it alongside the artifact files, and the serve sidecar
// exposes it. A consumer fetching artifacts over HTTP first reads the manifest,
// then fetches each listed file and verifies it against the recorded digest.
//
// ManifestKey is an optional contract key so the serve allowlist exposes it by
// construction; it is not part of the required artifact set a node mounts.
const ManifestKey = "manifest.json"

// Manifest is the served integrity + discovery document for a staged artifact
// directory. It lists every staged artifact file with its content digest so an
// HTTP consumer can verify exactly what it downloaded. connection.json is one
// of the listed files, so the manifest is the single entry point: read it,
// then fetch and verify each file (including connection.json) it names.
type Manifest struct {
	// SchemaVersion identifies the artifact bundle schema. It matches the
	// package SchemaVersion so producers and consumers agree on the wire shape.
	SchemaVersion string `json:"schemaVersion"`

	// Files maps each staged artifact key to its content digest, formatted as
	// "sha256:<64 hex>" (the same shape as the bundle DataHash).
	Files map[string]string `json:"files"`
}

// FileDigest returns the canonical "sha256:<hex>" digest of content.
func FileDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BuildManifest builds a Manifest over the staged files, keyed by artifact key
// with each value the sha256 digest of the file content. The manifest itself
// is never included as one of its own files.
func BuildManifest(files map[string][]byte) Manifest {
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		Files:         make(map[string]string, len(files)),
	}
	for name, content := range files {
		if name == ManifestKey {
			continue
		}
		manifest.Files[name] = FileDigest(content)
	}
	return manifest
}

// JSON renders the manifest as deterministic, indented JSON (map keys are
// emitted in sorted order by encoding/json), suitable for writing to disk and
// hashing reproducibly.
func (m Manifest) JSON() ([]byte, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal artifact manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

// Verify reports whether content matches the digest recorded for name. It
// returns an error when the file is absent from the manifest or the digest
// does not match, so a consumer can fail closed on a tampered or unexpected
// download.
func (m Manifest) Verify(name string, content []byte) error {
	want, ok := m.Files[name]
	if !ok {
		return fmt.Errorf("artifact %q is not listed in the manifest", name)
	}
	got := FileDigest(content)
	if got != want {
		return fmt.Errorf("artifact %q digest mismatch: got %s, want %s", name, got, want)
	}
	return nil
}

// SortedFileNames returns the manifest's artifact keys in deterministic order,
// for callers that iterate (e.g. a consumer fetching every listed file).
func (m Manifest) SortedFileNames() []string {
	names := make([]string, 0, len(m.Files))
	for name := range m.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
