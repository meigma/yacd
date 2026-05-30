package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// shutdownTimeout bounds graceful shutdown once the context is cancelled.
const shutdownTimeout = 5 * time.Second

// Options bundles the inputs [Run] consumes.
type Options struct {
	// Dir is the artifact directory to serve. It must exist and is resolved
	// (absolute + symlinks) once at startup.
	Dir string
	// Listen is the TCP address to listen on, e.g. ":8080".
	Listen string
	// ReadHeaderTimeout bounds reading request headers. Zero selects a safe
	// default.
	ReadHeaderTimeout time.Duration
}

// Run resolves the artifact directory and serves it read-only over HTTP until
// ctx is cancelled, then shuts the server down gracefully.
func Run(ctx context.Context, opts Options, out io.Writer) error {
	root, err := resolveDir(opts.Dir)
	if err != nil {
		return err
	}

	readHeaderTimeout := opts.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = 10 * time.Second
	}

	listener, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", opts.Listen, err)
	}

	srv := &http.Server{
		Handler:           &handler{root: root, allow: artifactKeySet()},
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()

	_, _ = fmt.Fprintf(out, "serving %s on %s\n", root, listener.Addr())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// resolveDir resolves dir to an absolute, symlink-free path and verifies it is
// an existing directory.
func resolveDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("--artifacts-dir is required")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve --artifacts-dir %s: %w", dir, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve --artifacts-dir %s: %w", dir, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat --artifacts-dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--artifacts-dir %s is not a directory", dir)
	}
	return resolved, nil
}

// handler serves files from root read-only. It is default-deny: a request is
// served only when its path is exactly one of the known artifact keys
// ([allow]), both as requested and after symlink resolution. This refuses
// traversal, directory listings, key material, backup files, and any future
// secret-shaped file by construction, rather than relying on a denylist.
type handler struct {
	root  string
	allow map[string]struct{}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Clean to an always-rooted path, which removes "." and ".." segments and
	// neutralizes traversal before we touch the filesystem.
	clean := path.Clean("/" + r.URL.Path)
	rel := strings.TrimPrefix(clean, "/")
	if !h.allowed(rel) {
		http.NotFound(w, r)
		return
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(h.root, filepath.FromSlash(rel)))
	if err != nil || !underRoot(h.root, resolved) {
		http.NotFound(w, r)
		return
	}

	// Re-check the resolved path against the allowlist: an in-root symlink with
	// an allowed name (e.g. configuration.yaml -> utxo-keys/pool.skey) would
	// otherwise resolve to a non-artifact file that is still under root.
	resolvedRel := strings.TrimPrefix(strings.TrimPrefix(resolved, h.root), string(os.PathSeparator))
	if !h.allowed(filepath.ToSlash(resolvedRel)) {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r) // no directory listing
		return
	}

	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// allowed reports whether rel is exactly one allowlisted artifact key. Artifact
// keys are flat filenames, so any path with a directory component is rejected.
func (h *handler) allowed(rel string) bool {
	_, ok := h.allow[rel]
	return ok
}

// artifactKeySet returns the set of artifact filenames the server may expose:
// the required and optional keys of the network artifact contract.
func artifactKeySet() map[string]struct{} {
	keys := append(networkartifacts.RequiredKeys(), networkartifacts.OptionalKeys()...)
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

// underRoot reports whether resolved is root itself or a descendant of it.
// Both are expected to be absolute, symlink-free paths.
func underRoot(root, resolved string) bool {
	return resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator))
}
