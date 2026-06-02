package toolbin

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Pin describes a version-pinned tool binary: which version, where to fetch the
// per-platform asset, and the expected digest for each platform. The SHA256 map
// is keyed by "<GOOS>/<GOARCH>" (for example "darwin/arm64"); AssetURL is a
// template containing the "{os}" and "{arch}" placeholders the resolver fills
// from runtime.GOOS/GOARCH. The digests are embedded at build time, not fetched
// at runtime, so a tampered or wrong download fails closed.
type Pin struct {
	Version  string
	AssetURL string
	SHA256   map[string]string
}

// Resolver returns the filesystem path to a verified, pinned tool binary,
// fetching it on demand when it is not already cached.
type Resolver interface {
	Resolve(ctx context.Context) (path string, err error)
}

// HTTPDoer is the HTTP seam the ghrelease adapter depends on. It is defined here
// rather than imported from the cli command package so the dependency direction
// stays adapter -> port. The standard *http.Client satisfies it; tests
// substitute a fake.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// DefaultDir resolves the directory yacd installs managed tool binaries into:
// $XDG_DATA_HOME/yacd/bin when XDG_DATA_HOME is set, otherwise
// $HOME/.local/share/yacd/bin. The ghrelease adapter takes the directory as a
// parameter so tests can substitute a temp dir; the composition root calls this
// for the real location.
func DefaultDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "yacd", "bin"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".local", "share", "yacd", "bin"), nil
}
