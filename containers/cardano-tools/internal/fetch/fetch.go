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
// download fails are reported and skipped.
func Run(ctx context.Context, opts Options, doer httpDoer, out io.Writer) error {
	files, ok := pinsFor(opts.Profile)
	if !ok {
		return fmt.Errorf("unknown profile %q (known: %s)", opts.Profile, strings.Join(knownProfiles, ", "))
	}

	if opts.DryRun {
		return writeDryRun(out, opts.Profile, files)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	for _, file := range files {
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
	}

	_, err := fmt.Fprintf(out, "fetched %s artifacts to %s\n", opts.Profile, opts.OutputDir)
	return err
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
