package ghrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/meigma/yacd/cli/internal/toolbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResponse is a canned HTTP response keyed by request URL.
type fakeResponse struct {
	status   int
	location string
	body     []byte
}

// fakeDoer serves canned responses and counts calls, so tests can assert that
// cache hits and pre-staged binaries never reach the network.
type fakeDoer struct {
	responses map[string]fakeResponse
	calls     int
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.calls++

	header := make(http.Header)
	resp, ok := f.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	}
	if resp.location != "" {
		header.Set("Location", resp.location)
	}

	return &http.Response{
		StatusCode: resp.status,
		Status:     fmt.Sprintf("%d %s", resp.status, http.StatusText(resp.status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(resp.body)),
	}, nil
}

const testAssetURL = "https://github.com/k3d-io/k3d/releases/download/test/k3d-{os}-{arch}"

func resolvedURL() string {
	return strings.NewReplacer("{os}", runtime.GOOS, "{arch}", runtime.GOARCH).Replace(testAssetURL)
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// pinFor builds a Pin whose digest for the current platform matches body.
func pinFor(body []byte) toolbin.Pin {
	return toolbin.Pin{
		Version:  "v0.0.0-test",
		AssetURL: testAssetURL,
		SHA256:   map[string]string{runtime.GOOS + "/" + runtime.GOARCH: digestOf(body)},
	}
}

func TestResolveDownloadsVerifiesAndInstalls(t *testing.T) {
	body := []byte("fake-k3d-binary")
	doer := &fakeDoer{responses: map[string]fakeResponse{
		resolvedURL(): {status: http.StatusOK, body: body},
	}}
	dir := t.TempDir()

	path, err := New(pinFor(body), dir, doer).Resolve(context.Background())
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "k3d-v0.0.0-test"), path)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "installed binary must be executable")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, content)
}

func TestResolveFollowsGitHubCDNRedirect(t *testing.T) {
	body := []byte("fake-k3d-binary-via-cdn")
	cdnURL := "https://release-assets.githubusercontent.com/github-production-release-asset/blob"
	doer := &fakeDoer{responses: map[string]fakeResponse{
		resolvedURL(): {status: http.StatusFound, location: cdnURL},
		cdnURL:        {status: http.StatusOK, body: body},
	}}
	dir := t.TempDir()

	path, err := New(pinFor(body), dir, doer).Resolve(context.Background())
	require.NoError(t, err)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, content)
}

func TestResolveRejectsRedirectToDisallowedHost(t *testing.T) {
	body := []byte("fake-k3d-binary")
	doer := &fakeDoer{responses: map[string]fakeResponse{
		resolvedURL(): {status: http.StatusFound, location: "https://evil.example.com/k3d"},
	}}
	dir := t.TempDir()

	_, err := New(pinFor(body), dir, doer).Resolve(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disallowed host")
	assertNoBinary(t, dir)
}

func TestResolveFailsClosedOnDigestMismatch(t *testing.T) {
	served := []byte("tampered-binary")
	pin := pinFor([]byte("the-binary-we-expected")) // digest is for different bytes
	doer := &fakeDoer{responses: map[string]fakeResponse{
		resolvedURL(): {status: http.StatusOK, body: served},
	}}
	dir := t.TempDir()

	_, err := New(pin, dir, doer).Resolve(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")
	assertNoBinary(t, dir)
}

func TestResolveSkipsFetchForPreStagedBinary(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "k3d")
	require.NoError(t, os.WriteFile(staged, []byte("operator-supplied"), 0o755))
	t.Setenv("YACD_K3D_PATH", staged)

	doer := &fakeDoer{}
	path, err := New(pinFor([]byte("unused")), t.TempDir(), doer).Resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, staged, path)
	assert.Zero(t, doer.calls, "pre-staged binary must skip the network")
}

func TestResolveReturnsCacheHitWithoutFetching(t *testing.T) {
	body := []byte("cached-k3d-binary")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "k3d-v0.0.0-test"), body, 0o755))

	doer := &fakeDoer{}
	path, err := New(pinFor(body), dir, doer).Resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "k3d-v0.0.0-test"), path)
	assert.Zero(t, doer.calls, "a matching cached binary must skip the network")
}

func TestResolveGarbageCollectsSupersededVersions(t *testing.T) {
	body := []byte("new-k3d-binary")
	dir := t.TempDir()
	stale := filepath.Join(dir, "k3d-v0.0.0-OLD")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o755))

	doer := &fakeDoer{responses: map[string]fakeResponse{
		resolvedURL(): {status: http.StatusOK, body: body},
	}}
	_, err := New(pinFor(body), dir, doer).Resolve(context.Background())
	require.NoError(t, err)

	_, err = os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "superseded binary must be garbage-collected")
	_, err = os.Stat(filepath.Join(dir, "k3d-v0.0.0-test"))
	assert.NoError(t, err, "current binary must remain")
}

func TestResolveRejectsUnsupportedPlatform(t *testing.T) {
	pin := toolbin.Pin{Version: "v0.0.0-test", AssetURL: testAssetURL, SHA256: map[string]string{}}
	_, err := New(pin, t.TempDir(), &fakeDoer{}).Resolve(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform")
}

func TestAllowedRedirectHost(t *testing.T) {
	assert.True(t, allowedRedirectHost("github.com"))
	assert.True(t, allowedRedirectHost("release-assets.githubusercontent.com"))
	assert.True(t, allowedRedirectHost("objects.githubusercontent.com"))
	assert.False(t, allowedRedirectHost("evil.example.com"))
	assert.False(t, allowedRedirectHost("githubusercontent.com.evil.com"))
}

func assertNoBinary(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "k3d-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "no binary or temp file should be left behind")
}
