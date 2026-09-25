package verify

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// lockFile takes the advisory file lock at path, waiting until ctx ends. When
// the lock is held, waiting, if set, is told the lock's name before the wait
// starts (see Session.waiting).
func lockFile(ctx context.Context, path string, waiting func(name string)) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	lock := flock.New(path, flock.SetPermissions(0600))
	locked, err := lock.TryLock()
	if err == nil && !locked {
		notifyWaiting(waiting, filepath.Base(path))
		locked, err = lock.TryLockContext(ctx, 20*time.Millisecond)
	}
	if err != nil || !locked {
		_ = lock.Close()
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	}
	return func() { _ = lock.Unlock() }, nil
}

func notifyWaiting(waiting func(name string), name string) {
	if waiting != nil {
		waiting(name)
	}
}
