package kube_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeKubeconfig writes a minimal two-context kubeconfig with the given
// current-context and returns its path.
func writeKubeconfig(t *testing.T, current string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	content := `apiVersion: v1
kind: Config
clusters:
- name: c1
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: ctx-a
  context:
    cluster: c1
- name: ctx-b
  context:
    cluster: c1
current-context: ` + current + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestCurrentAndSetContext(t *testing.T) {
	path := writeKubeconfig(t, "ctx-a")
	t.Setenv("KUBECONFIG", path)

	current, err := kube.CurrentContext()
	require.NoError(t, err)
	assert.Equal(t, "ctx-a", current)

	// Switching to another context mirrors `kubectl config use-context`.
	require.NoError(t, kube.SetCurrentContext(path, "ctx-b"))
	current, err = kube.CurrentContext()
	require.NoError(t, err)
	assert.Equal(t, "ctx-b", current)

	// Clearing (empty) returns the kubeconfig to "no current-context", which is
	// what teardown relies on when the user had no prior context.
	require.NoError(t, kube.SetCurrentContext(path, ""))
	current, err = kube.CurrentContext()
	require.NoError(t, err)
	assert.Equal(t, "", current, "an empty context must clear current-context")
}
