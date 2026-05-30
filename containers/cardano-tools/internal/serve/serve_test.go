package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newServer returns an httptest server backed by handler rooted at a temp dir
// pre-populated with files, plus the resolved root.
func newServer(t *testing.T, files map[string]string) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	root, err := resolveDir(dir)
	require.NoError(t, err)
	srv := httptest.NewServer(&handler{root: root})
	t.Cleanup(srv.Close)
	return srv, root
}

// get issues a GET against the server and returns the status code and body.
func get(t *testing.T, srv *httptest.Server, urlPath string) (int, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + urlPath)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body := make([]byte, 0, 64)
	buf := make([]byte, 64)
	for {
		n, readErr := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	return resp.StatusCode, string(body)
}

func TestServeReturnsKnownFile(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, map[string]string{"config.json": `{"ok":true}`})

	code, body := get(t, srv, "/config.json")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, `{"ok":true}`, body)
}

func TestServeRejectsTraversal(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, map[string]string{"config.json": "x"})

	// Traversal attempts that escape the root must 404. (A request like
	// /%2e%2e/config.json cleans back to /config.json inside the root and is
	// served safely; only paths resolving outside root are rejected.)
	for _, p := range []string{"/../../etc/passwd", "/..%2f..%2fetc%2fpasswd", "/etc/passwd"} {
		code, body := get(t, srv, p)
		assert.Equal(t, http.StatusNotFound, code, "path %s must 404", p)
		assert.NotContains(t, body, "root:", "path %s must not leak /etc/passwd", p)
	}
}

func TestServeRefusesSecretComponents(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, map[string]string{
		"config.json":              "ok",
		"pools-keys/pool1/kes.skey": "SECRET",
		"utxo-keys/utxo1.skey":      "SECRET",
	})

	for _, p := range []string{"/pools-keys/pool1/kes.skey", "/utxo-keys/utxo1.skey"} {
		code, body := get(t, srv, p)
		assert.Equal(t, http.StatusNotFound, code, "secret path %s must be refused", p)
		assert.NotContains(t, body, "SECRET")
	}
}

func TestServeRefusesSymlinkEscapingRoot(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}

	srv, root := newServer(t, map[string]string{"config.json": "ok"})
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("SECRET"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "leak")))

	code, body := get(t, srv, "/leak")
	assert.Equal(t, http.StatusNotFound, code)
	assert.NotContains(t, body, "SECRET")
}

func TestServeRefusesDirectoryListing(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, map[string]string{"sub/config.json": "ok"})

	code, _ := get(t, srv, "/sub")
	assert.Equal(t, http.StatusNotFound, code)

	code, _ = get(t, srv, "/")
	assert.Equal(t, http.StatusNotFound, code)
}

func TestResolveDirRejectsMissingAndNonDir(t *testing.T) {
	t.Parallel()

	_, err := resolveDir("")
	require.Error(t, err)

	_, err = resolveDir(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	_, err = resolveDir(file)
	require.Error(t, err)
}
