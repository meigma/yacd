package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/yacd/containers/cardano-tools/internal/artifactset"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// maxArtifactBytes bounds a single downloaded artifact. Public genesis is a few
// MB at most (mainnet byron-genesis is ~1.1 MB); the cap is a generous guard
// against a misbehaving or hostile source streaming unbounded data.
const maxArtifactBytes = 64 << 20 // 64 MiB

// outputFilePerm is the mode for downloaded artifact files.
const outputFilePerm = 0o644

// httpDoer is the HTTP seam Run depends on, satisfied by *http.Client and by
// test fakes.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Options bundles the inputs [Run] consumes.
type Options struct {
	// Profile is the public network to fetch (preview, preprod, or mainnet).
	Profile string
	// OutputDir is the directory the downloaded files are written into.
	OutputDir string
	// DryRun reports whether Run should print the resolved download manifest
	// instead of fetching anything.
	DryRun bool
}

// Run fetches the profile's artifacts through doer and writes them to
// OutputDir, hard-failing on any pinned-digest mismatch. Optional files whose
// download fails are reported and skipped. After the artifact files are
// written, Run completes the flat served directory by adding connection.json (a
// discovery document for HTTP consumers) and manifest.json (an integrity index
// over every file in the directory). manifest.json is written last so it covers
// connection.json.
func Run(ctx context.Context, opts Options, doer httpDoer, out io.Writer) error {
	pins, ok := pinsFor(opts.Profile)
	if !ok {
		return fmt.Errorf("unknown profile %q (known: %s)", opts.Profile, strings.Join(knownProfiles, ", "))
	}

	if opts.DryRun {
		return writeDryRun(out, opts.Profile, pins.files)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// written accumulates the bytes of every file placed in OutputDir so the
	// served manifest can be built without re-reading from disk. connectionKeys
	// maps each present artifact's connection key to its served filename for
	// connection.json's files map.
	written := make(map[string][]byte, len(pins.files)+2)
	connectionKeys := make(map[string]string, len(pins.files))

	for _, file := range pins.files {
		body, err := download(ctx, doer, file.url)
		if err != nil {
			if file.optional {
				_, _ = fmt.Fprintf(out, "skipped optional %s: %v\n", file.dest, err)
				continue
			}
			return fmt.Errorf("fetch %s: %w", file.dest, err)
		}

		if file.expectedSHA256 != "" {
			actual := sha256Hex(body)
			if actual != file.expectedSHA256 {
				return fmt.Errorf("pinned digest mismatch for %s: got sha256:%s, want sha256:%s", file.dest, actual, file.expectedSHA256)
			}
		}

		if err := writeArtifact(opts.OutputDir, file.dest, body); err != nil {
			return err
		}
		written[file.dest] = body
		if file.connectionKey != "" {
			connectionKeys[file.connectionKey] = file.dest
		}
	}

	connection, err := artifactset.RenderPublicConnection(artifactset.PublicConnection{
		Profile:              opts.Profile,
		NetworkMagic:         pins.networkMagic,
		RequiresNetworkMagic: pins.requiresNetworkMagic,
		Files:                connectionKeys,
	})
	if err != nil {
		return fmt.Errorf("render connection.json: %w", err)
	}
	if err := writeArtifact(opts.OutputDir, networkartifacts.ConnectionKey, []byte(connection)); err != nil {
		return err
	}
	written[networkartifacts.ConnectionKey] = []byte(connection)

	if err := writeManifest(opts.OutputDir, written); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "fetched %s artifacts to %s\n", opts.Profile, opts.OutputDir)
	return err
}

// writeManifest builds the integrity manifest over files and writes it to
// OutputDir/manifest.json. [networkartifacts.BuildManifest] skips manifest.json
// itself, so the caller must invoke this after every other file is written.
func writeManifest(dir string, files map[string][]byte) error {
	manifest := networkartifacts.BuildManifest(files)
	raw, err := manifest.JSON()
	if err != nil {
		return fmt.Errorf("render manifest.json: %w", err)
	}
	return writeArtifact(dir, networkartifacts.ManifestKey, raw)
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

// sha256Hex returns the lowercase hex SHA-256 digest of body.
func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// writeDryRun prints the resolved download manifest: each file's URL and
// whether it is digest-pinned.
func writeDryRun(out io.Writer, profile string, files []pinnedFile) error {
	if _, err := fmt.Fprintf(out, "profile: %s\n", profile); err != nil {
		return err
	}
	for _, file := range files {
		pin := "unpinned"
		if file.expectedSHA256 != "" {
			pin = "sha256:" + file.expectedSHA256
		}
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\n", file.dest, file.url, pin); err != nil {
			return err
		}
	}
	return nil
}
