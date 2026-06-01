package artifactsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// maxArtifactBytes bounds a single fetched artifact. Public genesis is a few MB
// at most (mainnet byron-genesis is ~1.1 MB); the cap is a generous guard
// against a misbehaving or hostile endpoint streaming unbounded data. It
// matches the fetch verb's bound.
const maxArtifactBytes = 64 << 20 // 64 MiB

// outputFilePerm is the mode for written artifact files.
const outputFilePerm = 0o644

// httpDoer is the HTTP seam Run depends on, satisfied by *http.Client and by
// test fakes.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Options bundles the inputs [Run] consumes.
type Options struct {
	// ServeURL is the base URL of a cardano-tools serve endpoint, e.g.
	// "http://<net>-artifacts.<ns>.svc.cluster.local:8090". Run reads
	// manifest.json and the artifact files relative to it.
	ServeURL string
	// OutputDir is the directory the verified files are written into.
	OutputDir string
	// DryRun reports whether Run should print the manifest's file list instead
	// of fetching anything.
	DryRun bool
}

// Run fetches manifest.json from the serve endpoint, then fetches and verifies
// every file the manifest names and writes it to OutputDir. It hard-fails on a
// non-200 response, a redirect (rejected by the caller-supplied client), a
// digest mismatch, a missing file, or an oversize body, so a tampered or
// incomplete bundle never reaches the consumer. The fetched manifest.json is
// written verbatim last so the output directory is self-describing and a re-run
// is idempotent.
func Run(ctx context.Context, opts Options, doer httpDoer, out io.Writer) error {
	if strings.TrimSpace(opts.ServeURL) == "" {
		return fmt.Errorf("serve URL is required")
	}

	manifestRaw, err := download(ctx, doer, artifactURL(opts.ServeURL, networkartifacts.ManifestKey))
	if err != nil {
		return fmt.Errorf("fetch %s: %w", networkartifacts.ManifestKey, err)
	}
	var manifest networkartifacts.Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return fmt.Errorf("parse %s: %w", networkartifacts.ManifestKey, err)
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("%s lists no files", networkartifacts.ManifestKey)
	}

	if opts.DryRun {
		return writeDryRun(out, opts.ServeURL, manifest)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	for _, name := range manifest.SortedFileNames() {
		body, err := download(ctx, doer, artifactURL(opts.ServeURL, name))
		if err != nil {
			return fmt.Errorf("fetch %s: %w", name, err)
		}
		if err := manifest.Verify(name, body); err != nil {
			return err
		}
		if err := writeArtifact(opts.OutputDir, name, body); err != nil {
			return err
		}
	}

	// Write manifest.json verbatim (the fetched bytes), not a rebuilt manifest,
	// so the served digests are preserved exactly. BuildManifest excludes
	// manifest.json from its own file list, so it never appears in the loop
	// above; write it here to complete the self-describing directory.
	if err := writeArtifact(opts.OutputDir, networkartifacts.ManifestKey, manifestRaw); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "synced %d artifacts from %s to %s\n", len(manifest.Files), opts.ServeURL, opts.OutputDir)
	return err
}

// artifactURL joins a serve base URL and an artifact filename. The filename is a
// contract key with no path separators, so simple slash joining is safe.
func artifactURL(base, name string) string {
	return strings.TrimRight(base, "/") + "/" + name
}

// download issues a GET for url through doer and returns the body, bounding the
// read and rejecting non-200 responses.
func download(ctx context.Context, doer httpDoer, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxArtifactBytes {
		return nil, fmt.Errorf("artifact exceeds %d bytes", maxArtifactBytes)
	}
	return body, nil
}

// writeArtifact writes body to name under dir, refusing names that escape dir.
// The manifest's keys are contract filenames, but verifying the name here keeps
// the write fail-closed against a hostile manifest.
func writeArtifact(dir, name string, body []byte) error {
	if name == "" || strings.ContainsRune(name, '/') || strings.ContainsRune(name, os.PathSeparator) || name == "." || name == ".." {
		return fmt.Errorf("refusing to write artifact with unsafe name %q", name)
	}
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, body, outputFilePerm); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// writeDryRun prints the serve URL and the manifest's file list, fetching
// nothing.
func writeDryRun(out io.Writer, serveURL string, manifest networkartifacts.Manifest) error {
	if _, err := fmt.Fprintf(out, "serve-url: %s\n", serveURL); err != nil {
		return err
	}
	for _, name := range manifest.SortedFileNames() {
		if _, err := fmt.Fprintf(out, "%s\t%s\n", name, manifest.Files[name]); err != nil {
			return err
		}
	}
	return nil
}
