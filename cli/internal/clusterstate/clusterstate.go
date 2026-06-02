package clusterstate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Record is the supplementary bookkeeping the cluster runtime cannot hold.
type Record struct {
	ClusterName    string `json:"clusterName"`
	Context        string `json:"context"`
	PriorContext   string `json:"priorContext"`
	K3dVersion     string `json:"k3dVersion"`
	KubeconfigPath string `json:"kubeconfigPath"`
}

// Store loads, saves, and clears the managed-cluster record and holds a process
// lock scoped to the managed cluster.
type Store interface {
	// Load returns the record and true, or a zero record and false when none is
	// stored. A corrupt record is returned as an error.
	Load() (Record, bool, error)

	// Save persists the record.
	Save(Record) error

	// Clear removes the record. It is idempotent.
	Clear() error

	// Lock acquires the managed-cluster file lock, blocking until it is held or
	// ctx is done. The returned release function unlocks it.
	Lock(ctx context.Context) (release func() error, err error)
}

// DefaultDir resolves the directory the CLI stores managed-cluster state in:
// $XDG_STATE_HOME/yacd when XDG_STATE_HOME is set, otherwise
// $HOME/.local/state/yacd.
func DefaultDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); dir != "" {
		return filepath.Join(dir, "yacd"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".local", "state", "yacd"), nil
}
