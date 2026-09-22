package verify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type countingExecutor struct {
	calls    int
	status   Status
	artifact string
}

func (e *countingExecutor) Execute(ctx context.Context, req Request) Result {
	e.calls++
	if e.artifact != "" {
		_ = os.WriteFile(filepath.Join(req.Source, e.artifact), []byte("report"), 0600)
	}
	return Result{Status: e.status, Stdout: "original diagnostics"}
}

func cacheRequest(t *testing.T) Request {
	t.Helper()
	req := nativeRequest(t)
	req.Shared = t.TempDir()
	req.Check.Command.Cache = true
	req.Check.Command.RerunArgs = req.Check.Command.Args
	req.Environment.Identity = "fixture-v1"
	req.Target.Inputs = []string{"input"}
	if err := os.WriteFile(filepath.Join(req.Source, "input"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestResultCacheInvalidationAndFreshness(t *testing.T) {
	req := cacheRequest(t)
	cache := &Cache{Dir: t.TempDir()}
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: cache, Executor: executor}

	first := runner.Execute(context.Background(), req)
	if first.Cache.Status != CacheMiss || executor.calls != 1 {
		t.Fatalf("cold: %+v", first)
	}

	second := runner.Execute(context.Background(), req)
	if second.Cache.Status != CacheHit || executor.calls != 1 || !second.VerifiedAt.Equal(first.VerifiedAt) {
		t.Fatalf("warm: %+v", second)
	}
	if err := os.WriteFile(filepath.Join(req.Source, "unrelated"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheHit {
		t.Fatalf("unrelated edit: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(req.Source, "input"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheMiss || executor.calls != 2 {
		t.Fatalf("input edit: %+v", result)
	}

	req.RerunChecks = true
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheFresh || executor.calls != 3 {
		t.Fatalf("fresh: %+v", result)
	}

	req.RerunChecks = false
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheHit {
		t.Fatalf("fresh should populate ordinary result: %+v", result)
	}

	req.Check.Command.Env = map[string]string{"FEATURE": "other"}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheMiss {
		t.Fatalf("variant: %+v", result)
	}
	if err := os.Remove(filepath.Join(req.Source, "input")); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheMiss {
		t.Fatalf("deletion: %+v", result)
	}
}

func TestCacheArtifactsCorruptionAndFailedResults(t *testing.T) {
	req := cacheRequest(t)
	req.Check.Command.Artifacts = []string{"report.txt"}
	cache := &Cache{Dir: t.TempDir()}
	executor := &countingExecutor{status: StatusPassed, artifact: "report.txt"}
	runner := CachedExecutor{Cache: cache, Executor: executor}

	first := runner.Execute(context.Background(), req)
	if err := os.Remove(filepath.Join(req.Source, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheHit || result.Stdout != "original diagnostics" {
		t.Fatalf("artifact restore: %+v", result)
	}
	if data, err := os.ReadFile(filepath.Join(req.Source, "report.txt")); err != nil || string(data) != "report" {
		t.Fatalf("missing restored report: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(cache.Dir, "results", first.Cache.Key+".json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheMiss || executor.calls != 2 {
		t.Fatalf("corruption: %+v", result)
	}

	req.Check.Command.Args = []string{"different"}
	executor.status = StatusFailed
	for range 2 {
		if result := runner.Execute(context.Background(), req); result.Status != StatusFailed || result.Cache.Status == CacheHit {
			t.Fatalf("cached failure: %+v", result)
		}
	}
	if executor.calls != 4 {
		t.Fatalf("failure reused: %d", executor.calls)
	}
}

func TestPersistentPreparationAcrossExecutors(t *testing.T) {
	req := cacheRequest(t)
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "echo prep >> count; printf environment > ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	req.Check.Command.Args = []string{"/bin/sh", "-c", "test -f ready"}
	req.Check.Command.RerunArgs = req.Check.Command.Args
	cache := &Cache{Dir: t.TempDir()}
	for i := range 2 {
		result := (&Native{Cache: cache}).Execute(context.Background(), req)
		if result.Status != StatusPassed || len(result.Stages) != 1 || result.Stages[0].Reused != (i == 1) {
			t.Fatalf("stage %d: %+v", i, result)
		}
	}
	if err := os.WriteFile(filepath.Join(req.Source, "ready"), []byte("overwritten by another environment"), 0600); err != nil {
		t.Fatal(err)
	}

	result := (&Native{Cache: cache}).Execute(context.Background(), req)
	if result.Status != StatusPassed || result.Stages[0].Reused {
		t.Fatalf("overwritten setup reused: %+v", result)
	}

	req.RerunChecks = true

	result = (&Native{Cache: cache}).Execute(context.Background(), req)
	if result.Status != StatusPassed || !result.Stages[0].Reused {
		t.Fatalf("fresh erased preparation: %+v", result)
	}
}

func TestCacheLockCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := lockFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if release, err := lockFile(ctx, path); err == nil {
		release()
		t.Fatal("conflicting writer entered lock")
	}
}

func TestArtifactRestorationRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := checkOutputPath(root, "alias/report"); err == nil {
		t.Fatal("artifact escaped through alias")
	}
}

func TestRecordLayoutMatchesUntaggedEnvelope(t *testing.T) {
	data := json.RawMessage(`{"Key":"untagged"}`)
	untagged := `{"Checksum":"` + digest(data) + `","Data":` + string(data) + `}`
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, []byte(untagged), 0600); err != nil {
		t.Fatal(err)
	}

	var e entry
	if err := readRecord(path, &e); err != nil || e.Key != "untagged" {
		t.Fatalf("record written before the envelope had tags: entry %+v, error %v", e, err)
	}

	if err := writeRecord(path, map[string]string{"Key": "untagged"}); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != untagged {
		t.Fatalf("writeRecord changed the on-disk layout:\n got %s\nwant %s", written, untagged)
	}
}

func TestInputSymlinkDisablesResultCache(t *testing.T) {
	req := cacheRequest(t)
	if err := os.Symlink("input", filepath.Join(req.Source, "alias")); err != nil {
		t.Fatal(err)
	}

	req.Target.Inputs = []string{"alias"}
	executor := &countingExecutor{status: StatusPassed}

	result := (CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}).Execute(context.Background(), req)
	if result.Status != StatusPassed || result.Cache.Status != CacheUnavailable {
		t.Fatalf("unsafe input cached: %+v", result)
	}
}

func TestUnpinnedEnvironmentDoesNotPersistPreparation(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Identity = ""
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "printf environment > ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	cache := &Cache{Dir: t.TempDir()}
	for range 2 {
		result := (&Native{Cache: cache}).Execute(context.Background(), req)
		if result.Status != StatusPassed || result.Stages[0].Reused {
			t.Fatalf("unpinned environment persisted: %+v", result)
		}
	}
}

type verdictCachingExecutor struct{ failFresh bool }

func (e *verdictCachingExecutor) Execute(ctx context.Context, req Request) Result {
	if req.RerunChecks && e.failFresh {
		return Result{Status: StatusFailed}
	}
	return Result{Status: StatusPassed}
}

func TestFreshFailureBypassesUnderlyingVerdictCache(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Executor = ExecutorDagger
	executor := &verdictCachingExecutor{}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}
	if result := runner.Execute(context.Background(), req); result.Status != StatusPassed {
		t.Fatal(result)
	}

	executor.failFresh = true
	req.RerunChecks = true
	if result := runner.Execute(context.Background(), req); result.Status != StatusFailed {
		t.Fatal(result)
	}

	req.RerunChecks = false

	result := runner.Execute(context.Background(), req)
	if result.Status != StatusFailed || result.Cache.Status != CacheFresh {
		t.Fatalf("lower-level cached success hid known failure: %+v", result)
	}
}

// A Dagger check's result is reusable without opting in, because the runner's
// pinned toolchain identifies it; a native command is reused only when it opts
// in with command.cache.
func TestDaggerChecksAreCacheableWithoutOptingIn(t *testing.T) {
	req := cacheRequest(t)
	req.Check = Check{Kind: CheckGoHTTP}
	req.Environment = Environment{Executor: ExecutorDagger}
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

	runner.Execute(t.Context(), req)
	if result := runner.Execute(t.Context(), req); result.Cache.Status != CacheHit || executor.calls != 1 {
		t.Fatalf("a repeated Dagger check must be reused: %+v after %d calls", result, executor.calls)
	}

	native := cacheRequest(t)
	native.Check.Command.Cache = false
	runner.Execute(t.Context(), native)
	if result := runner.Execute(t.Context(), native); result.Cache.Status != CacheDisabled || executor.calls != 3 {
		t.Fatalf("a native command without command.cache must not be reused: %+v after %d calls", result, executor.calls)
	}
}

// Without a result cache there is nothing to reuse, and nothing to lock.
func TestNoCacheExecutesEveryTime(t *testing.T) {
	req := cacheRequest(t)
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Executor: executor}

	runner.Execute(t.Context(), req)
	if result := runner.Execute(t.Context(), req); result.Cache.Status != CacheDisabled || executor.calls != 2 {
		t.Fatalf("without a cache every run must execute: %+v after %d calls", result, executor.calls)
	}
}

// Native checks in one workspace share the working tree, so they take its
// lock; a Dagger check runs on its own copy of the source and must not wait.
func TestOnlyNativeChecksLockTheWorkspace(t *testing.T) {
	req := cacheRequest(t)
	cache := &Cache{Dir: t.TempDir()}
	unlock, err := lockFile(t.Context(), filepath.Join(cache.Dir, "locks", "workspace-"+digest(req.Source)))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	runner := CachedExecutor{Cache: cache, Executor: &countingExecutor{status: StatusPassed}}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if result := runner.Execute(ctx, req); result.Status != StatusError || !strings.Contains(result.Error, "cannot lock native workspace") {
		t.Fatalf("a native check ran while its workspace was locked: %+v", result)
	}

	dagger := req
	dagger.Check = Check{Kind: CheckGoHTTP}
	dagger.Environment = Environment{Executor: ExecutorDagger}
	if result := runner.Execute(t.Context(), dagger); result.Status != StatusPassed {
		t.Fatalf("a Dagger check waited on the native workspace lock: %+v", result)
	}
}
