package verify

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// lockWaiting, when set, is told the name of every lock a caller finds held and
// starts waiting for. Tests use it to act while a check is known to be blocked.
var lockWaiting func(name string)

func waitingOn(name string) {
	if hook := lockWaiting; hook != nil {
		hook(name)
	}
}

func lockFile(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	lock := flock.New(path, flock.SetPermissions(0600))
	locked, err := lock.TryLock()
	if err == nil && !locked {
		waitingOn(filepath.Base(path))
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
