package kube

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientRequiresHostAndToken(t *testing.T) {
	t.Parallel()

	_, err := NewClient(Config{TokenPath: "/var/run/token"})
	require.Error(t, err, "missing APIURL must fail")

	_, err = NewClient(Config{APIURL: "https://api"})
	require.Error(t, err, "missing TokenPath must fail")

	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("token"), 0o600))
	_, err = NewClient(Config{APIURL: "https://api", TokenPath: tokenPath})
	require.NoError(t, err)
}

func TestMarshalMergePatchIndentedFraming(t *testing.T) {
	t.Parallel()

	body, err := MarshalMergePatchIndented(ConfigMapPatch{
		SetData:     map[string]string{"configuration.yaml": "data"},
		PruneData:   []string{"stale.json"},
		Annotations: map[string]string{"yacd.meigma.io/artifact-data-hash": "sha256:abc"},
	})
	require.NoError(t, err)

	got := string(body)
	// Set data becomes a value, pruned keys become explicit null, and the
	// annotation lands under metadata.
	assert.Contains(t, got, `"configuration.yaml": "data"`)
	assert.Contains(t, got, `"stale.json": null`)
	assert.Contains(t, got, `"yacd.meigma.io/artifact-data-hash": "sha256:abc"`)
}

func TestBuildBodyOmitsDataWhenEmpty(t *testing.T) {
	t.Parallel()

	body := buildBody(ConfigMapPatch{Annotations: map[string]string{"a": "b"}})
	assert.Nil(t, body.Data, "no set or prune keys should leave data nil so the merge patch omits it")
}
