package file

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/meigma/yacd/cli/internal/clusterstate"
)

const (
	recordName = "cluster.json"
	lockName   = "cluster.lock"
	dirPerm    = 0o700
	filePerm   = 0o600
)

// Store is the filesystem-backed clusterstate.Store.
type Store struct {
	dir string
}

// New constructs a Store rooted at dir (typically clusterstate.DefaultDir()).
func New(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) recordPath() string { return filepath.Join(s.dir, recordName) }

// Load reads the managed-cluster record. A missing record returns (zero, false,
// nil); a corrupt record is an error.
func (s *Store) Load() (clusterstate.Record, bool, error) {
	data, err := os.ReadFile(s.recordPath())
	if err != nil {
		if os.IsNotExist(err) {
			return clusterstate.Record{}, false, nil
		}
		return clusterstate.Record{}, false, fmt.Errorf("read cluster record: %w", err)
	}

	var record clusterstate.Record
	if err := json.Unmarshal(data, &record); err != nil {
		return clusterstate.Record{}, false, fmt.Errorf("parse cluster record: %w", err)
	}

	return record, true, nil
}

// Save persists the record, writing atomically via a temp file and rename.
func (s *Store) Save(record clusterstate.Record) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", s.dir, err)
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cluster record: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.dir, recordName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp record: %w", err)
	}
	tmpName := tmp.Name()
	saved := false
	defer func() {
		if !saved {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp record: %w", err)
	}
	if err := os.Chmod(tmpName, filePerm); err != nil {
		return fmt.Errorf("chmod temp record: %w", err)
	}
	if err := os.Rename(tmpName, s.recordPath()); err != nil {
		return fmt.Errorf("install cluster record: %w", err)
	}
	saved = true

	return nil
}

// Clear removes the record. It is idempotent and leaves the lock file in place.
func (s *Store) Clear() error {
	if err := os.Remove(s.recordPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear cluster record: %w", err)
	}

	return nil
}
