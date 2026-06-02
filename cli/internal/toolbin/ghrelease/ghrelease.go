package ghrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meigma/yacd/cli/internal/toolbin"
)

const (
	// maxBinaryBytes caps a download so a runaway or wrong body fails closed
	// rather than exhausting memory.
	maxBinaryBytes = 256 << 20 // 256 MiB

	// binaryFilePerm makes the installed binary executable.
	binaryFilePerm = 0o755

	// installDirPerm is the mode of the install directory.
	installDirPerm = 0o755

	// maxRedirects bounds redirect following while fetching a release asset.
	maxRedirects = 10

	// preStagedEnv lets an operator point at a k3d binary and skip the fetch.
	preStagedEnv = "YACD_K3D_PATH"
)

// Resolver implements toolbin.Resolver against a GitHub release.
type Resolver struct {
	pin  toolbin.Pin
	dir  string
	http toolbin.HTTPDoer
}

// New constructs a Resolver for pin, installing into dir and downloading through
// doer. The directory and HTTP client are injected so tests can use a temp dir
// and a fake doer; the composition root passes DefaultK3dPin, toolbin.DefaultDir,
// and an *http.Client configured to surface redirects (see DefaultHTTPClient).
func New(pin toolbin.Pin, dir string, doer toolbin.HTTPDoer) *Resolver {
	return &Resolver{pin: pin, dir: dir, http: doer}
}

// DefaultHTTPClient returns an HTTP client suitable for New: it does not auto-
// follow redirects, so the resolver can apply its own GitHub-host allowlist to
// each hop. The returned client satisfies toolbin.HTTPDoer.
func DefaultHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Resolve returns the path to a verified, pinned k3d binary, fetching it when it
// is not already cached.
func (r *Resolver) Resolve(ctx context.Context) (string, error) {
	if path, ok, err := preStaged(); err != nil || ok {
		return path, err
	}

	osArch := runtime.GOOS + "/" + runtime.GOARCH
	want, ok := r.pin.SHA256[osArch]
	if !ok {
		return "", fmt.Errorf("toolbin: unsupported platform %s for k3d %s", osArch, r.pin.Version)
	}

	target := filepath.Join(r.dir, "k3d-"+r.pin.Version)
	if matched, err := fileMatchesDigest(target, want); err == nil && matched {
		return target, nil // cache hit
	}

	assetURL := strings.NewReplacer("{os}", runtime.GOOS, "{arch}", runtime.GOARCH).Replace(r.pin.AssetURL)
	body, err := r.fetch(ctx, assetURL)
	if err != nil {
		return "", err
	}

	if got := sha256Hex(body); got != want {
		return "", fmt.Errorf(
			"toolbin: k3d %s digest mismatch for %s: got sha256:%s, want sha256:%s",
			r.pin.Version, osArch, got, want,
		)
	}

	if err := install(r.dir, target, body); err != nil {
		return "", err
	}
	if err := r.gc(target); err != nil {
		return "", err
	}

	return target, nil
}

// fetch downloads assetURL, following redirects only to GitHub download hosts.
func (r *Resolver) fetch(ctx context.Context, assetURL string) ([]byte, error) {
	current := assetURL
	for hop := 0; hop <= maxRedirects; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, fmt.Errorf("toolbin: build request for %s: %w", current, err)
		}

		resp, err := r.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("toolbin: fetch %s: %w", current, err)
		}

		if isRedirect(resp.StatusCode) {
			next, err := redirectTarget(current, resp)
			_ = resp.Body.Close()
			if err != nil {
				return nil, err
			}
			current = next
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("toolbin: fetch %s: unexpected status %s", current, resp.Status)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBinaryBytes+1))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("toolbin: read %s: %w", current, err)
		}
		if int64(len(body)) > maxBinaryBytes {
			return nil, fmt.Errorf("toolbin: %s exceeds the %d byte cap", current, maxBinaryBytes)
		}

		return body, nil
	}

	return nil, fmt.Errorf("toolbin: too many redirects fetching %s", assetURL)
}

// redirectTarget validates a redirect response and returns the next URL. It
// rejects redirects to any host outside the GitHub download allowlist; the
// embedded digest is the integrity guard, but host-pinning keeps a redirect from
// silently fetching from an attacker-controlled origin.
func redirectTarget(current string, resp *http.Response) (string, error) {
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("toolbin: redirect from %s without a Location header", current)
	}

	base, err := url.Parse(current)
	if err != nil {
		return "", fmt.Errorf("toolbin: parse %s: %w", current, err)
	}
	ref, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("toolbin: parse redirect location %q: %w", location, err)
	}
	next := base.ResolveReference(ref)

	if !allowedRedirectHost(next.Hostname()) {
		return "", fmt.Errorf("toolbin: refusing redirect to disallowed host %q", next.Hostname())
	}

	return next.String(), nil
}

// allowedRedirectHost reports whether host is a GitHub release-download host.
// GitHub release assets redirect to release-assets.githubusercontent.com.
func allowedRedirectHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" ||
		host == "githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}

// isRedirect reports whether status is a redirect the resolver follows.
func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// preStaged returns an operator-supplied binary path when YACD_K3D_PATH is set,
// short-circuiting the fetch.
func preStaged() (string, bool, error) {
	path := strings.TrimSpace(os.Getenv(preStagedEnv))
	if path == "" {
		return "", false, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("toolbin: %s=%s: %w", preStagedEnv, path, err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("toolbin: %s=%s is a directory, not a binary", preStagedEnv, path)
	}

	return path, true, nil
}

// install atomically writes body to target with the executable bit, via a temp
// file and rename so a torn download is never observed as a valid binary.
func install(dir, target string, body []byte) error {
	if err := os.MkdirAll(dir, installDirPerm); err != nil {
		return fmt.Errorf("toolbin: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "k3d-*.tmp")
	if err != nil {
		return fmt.Errorf("toolbin: create temp binary: %w", err)
	}
	tmpName := tmp.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("toolbin: write temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("toolbin: close temp binary: %w", err)
	}
	if err := os.Chmod(tmpName, binaryFilePerm); err != nil {
		return fmt.Errorf("toolbin: chmod temp binary: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("toolbin: install %s: %w", target, err)
	}
	installed = true

	return nil
}

// gc removes superseded k3d binaries (and any leftover temp files) in the
// install directory, keeping only the current target.
func (r *Resolver) gc(keep string) error {
	matches, err := filepath.Glob(filepath.Join(r.dir, "k3d-*"))
	if err != nil {
		return fmt.Errorf("toolbin: scan %s: %w", r.dir, err)
	}

	for _, path := range matches {
		if path == keep {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("toolbin: garbage-collect %s: %w", path, err)
		}
	}

	return nil
}

// fileMatchesDigest reports whether the file at path hashes to want.
func fileMatchesDigest(path, want string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxBinaryBytes+1)); err != nil {
		return false, err
	}

	return hex.EncodeToString(hash.Sum(nil)) == want, nil
}

// sha256Hex returns the hex-encoded SHA256 of body.
func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
