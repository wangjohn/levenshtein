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
	executor := &countingExecutor{status: "passed"}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}
	if got := runner.Execute(context.Background(), req); got.Status != "passed" {
		t.Fatal(got)
	}

	req.Fresh = true
	executor.status = "failed"
	if got := runner.Execute(context.Background(), req); got.Status != "failed" {
		t.Fatal(got)
	}

	req.Fresh = false
	if got := runner.Execute(context.Background(), req); got.Status == "passed" {
		t.Fatalf("returned older green after fresh failure: %+v; calls=%d", got, executor.calls)
	}
}

func TestReviewDaggerImplementationFilesAreInputs(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Executor = "dagger"
	before, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(req.Shared, "runner"), 0755); err != nil {
		t.Fatal(err)
	}
	// Extra files in package main execute in the Dagger module, too.
	if err := os.WriteFile(filepath.Join(req.Shared, "runner", "extra.go"), []byte("package main\nfunc init() { panic(\"bad adapter\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("Dagger runtime change did not invalidate verification")
	}
}

func TestReviewInputsChangedDuringCacheLockWait(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Executor = "dagger"
	executor := &countingExecutor{status: "passed"}
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
	if got.Cache.Status == "hit" {
		t.Fatalf("returned old cached pass after source changed during lock wait: %+v", got)
	}
}
