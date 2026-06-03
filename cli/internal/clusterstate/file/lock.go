package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// lockRetryInterval is how often Lock retries while another holder owns the
// managed-cluster lock.
const lockRetryInterval = 100 * time.Millisecond

// Lock acquires the managed-cluster file lock, retrying until it is held or ctx
// is done. The returned release function unlocks it. A ctx cancelled while the
// lock is contended returns ctx's error.
func (s *Store) Lock(ctx context.Context) (func() error, error) {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("create %s: %w", s.dir, err)
	}

	// Return the bare cause; the caller (lifecycle.Manager) owns the
	// "acquire cluster lock:" context so the message is not wrapped twice.
	lock := flock.New(filepath.Join(s.dir, lockName))
	acquired, err := lock.TryLockContext(ctx, lockRetryInterval)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, errors.New("not acquired")
	}

	return lock.Unlock, nil
}
