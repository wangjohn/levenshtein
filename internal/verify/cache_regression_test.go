package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewFreshFailureInvalidatesOldSuccess(t *testing.T) {
	req := cacheRequest(t)
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}
	if got := runner.Execute(context.Background(), req); got.Status != StatusPassed {
		t.Fatal(got)
	}

	req.RerunChecks = true
	executor.status = StatusFailed
	if got := runner.Execute(context.Background(), req); got.Status != StatusFailed {
		t.Fatal(got)
	}

	req.RerunChecks = false
	if got := runner.Execute(context.Background(), req); got.Status == StatusPassed {
		t.Fatalf("returned older green after fresh failure: %+v; calls=%d", got, executor.calls)
	}
}

func TestReviewDaggerImplementationFilesAreInputs(t *testing.T) {
	for _, file := range []string{"runner/extra.go", "sdk/patched-go/src/patched_go/__init__.py"} {
		t.Run(file, func(t *testing.T) {
			req := cacheRequest(t)
			req.Environment.Executor = ExecutorDagger
			before, err := fingerprint(req)
			if err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(req.Shared, file)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("changed implementation"), 0644); err != nil {
				t.Fatal(err)
			}

			after, err := fingerprint(req)
			if err != nil {
				t.Fatal(err)
			}
			if before == after {
				t.Fatal("Dagger runtime or SDK change did not invalidate verification")
			}
		})
	}
}

func TestReviewInputsChangedDuringCacheLockWait(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Executor = ExecutorDagger
	executor := &countingExecutor{status: StatusPassed}
	cache := &Cache{Dir: t.TempDir()}
	runner := CachedExecutor{Cache: cache, Executor: executor}

	first := runner.Execute(context.Background(), req)
	unlock, err := lockFile(context.Background(), filepath.Join(cache.Dir, "locks", "result-"+first.Cache.Key))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Result, 1)
	go func() { done <- runner.Execute(context.Background(), req) }()
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(req.Source, "input"), []byte("changed while waiting"), 0600); err != nil {
		t.Fatal(err)
	}
	unlock()
	got := <-done
	if got.Cache.Status == CacheHit {
		t.Fatalf("returned old cached pass after source changed during lock wait: %+v", got)
	}
}
