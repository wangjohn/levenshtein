package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type countingExecutor struct {
	calls    int
	status   string
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
	req.Check.Cache = true
	req.Check.FreshCommand = req.Check.Command
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
	executor := &countingExecutor{status: "passed"}
	runner := CachedExecutor{Cache: cache, Executor: executor}
	first := runner.Execute(context.Background(), req)
	if first.Cache.Status != "miss" || executor.calls != 1 {
		t.Fatalf("cold: %+v", first)
	}
	second := runner.Execute(context.Background(), req)
	if second.Cache.Status != "hit" || executor.calls != 1 || !second.VerifiedAt.Equal(first.VerifiedAt) {
		t.Fatalf("warm: %+v", second)
	}
	if err := os.WriteFile(filepath.Join(req.Source, "unrelated"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "hit" {
		t.Fatalf("unrelated edit: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(req.Source, "input"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "miss" || executor.calls != 2 {
		t.Fatalf("input edit: %+v", result)
	}
	req.Fresh = true
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "fresh" || executor.calls != 3 {
		t.Fatalf("fresh: %+v", result)
	}
	req.Fresh = false
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "hit" {
		t.Fatalf("fresh should populate ordinary result: %+v", result)
	}
	req.Check.Env = map[string]string{"FEATURE": "other"}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "miss" {
		t.Fatalf("variant: %+v", result)
	}
	if err := os.Remove(filepath.Join(req.Source, "input")); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "miss" {
		t.Fatalf("deletion: %+v", result)
	}
}
func TestCacheArtifactsCorruptionAndFailedResults(t *testing.T) {
	req := cacheRequest(t)
	req.Check.Artifacts = []string{"report.txt"}
	cache := &Cache{Dir: t.TempDir()}
	executor := &countingExecutor{status: "passed", artifact: "report.txt"}
	runner := CachedExecutor{Cache: cache, Executor: executor}
	first := runner.Execute(context.Background(), req)
	if err := os.Remove(filepath.Join(req.Source, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "hit" || result.Stdout != "original diagnostics" {
		t.Fatalf("artifact restore: %+v", result)
	}
	if data, err := os.ReadFile(filepath.Join(req.Source, "report.txt")); err != nil || string(data) != "report" {
		t.Fatalf("missing restored report: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(cache.Dir, "results", first.Cache.Key+".json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runner.Execute(context.Background(), req); result.Cache.Status != "miss" || executor.calls != 2 {
		t.Fatalf("corruption: %+v", result)
	}
	req.Check.Command = []string{"different"}
	executor.status = "failed"
	for i := 0; i < 2; i++ {
		if result := runner.Execute(context.Background(), req); result.Status != "failed" || result.Cache.Status == "hit" {
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
	req.Check.Command = []string{"/bin/sh", "-c", "test -f ready"}
	req.Check.FreshCommand = req.Check.Command
	cache := &Cache{Dir: t.TempDir()}
	for i := 0; i < 2; i++ {
		result := (&Native{Cache: cache}).Execute(context.Background(), req)
		if result.Status != "passed" || len(result.Stages) != 1 || result.Stages[0].Reused != (i == 1) {
			t.Fatalf("stage %d: %+v", i, result)
		}
	}
	if err := os.WriteFile(filepath.Join(req.Source, "ready"), []byte("overwritten by another environment"), 0600); err != nil {
		t.Fatal(err)
	}
	result := (&Native{Cache: cache}).Execute(context.Background(), req)
	if result.Status != "passed" || result.Stages[0].Reused {
		t.Fatalf("overwritten setup reused: %+v", result)
	}
	req.Fresh = true
	result = (&Native{Cache: cache}).Execute(context.Background(), req)
	if result.Status != "passed" || !result.Stages[0].Reused {
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
	if _, err := outputPath(root, "alias/report"); err == nil {
		t.Fatal("artifact escaped through alias")
	}
}
func TestInputSymlinkDisablesResultCache(t *testing.T) {
	req := cacheRequest(t)
	if err := os.Symlink("input", filepath.Join(req.Source, "alias")); err != nil {
		t.Fatal(err)
	}
	req.Target.Inputs = []string{"alias"}
	executor := &countingExecutor{status: "passed"}
	result := (CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}).Execute(context.Background(), req)
	if result.Status != "passed" || result.Cache.Status != "unavailable" {
		t.Fatalf("unsafe input cached: %+v", result)
	}
}

func TestUnpinnedEnvironmentDoesNotPersistPreparation(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Identity = ""
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "printf environment > ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	cache := &Cache{Dir: t.TempDir()}
	for i := 0; i < 2; i++ {
		result := (&Native{Cache: cache}).Execute(context.Background(), req)
		if result.Status != "passed" || result.Stages[0].Reused {
			t.Fatalf("unpinned environment persisted: %+v", result)
		}
	}
}

type verdictCachingExecutor struct{ failFresh bool }

func (e *verdictCachingExecutor) Execute(ctx context.Context, req Request) Result {
	if req.Fresh && e.failFresh {
		return Result{Status: "failed"}
	}
	return Result{Status: "passed"}
}
func TestFreshFailureBypassesUnderlyingVerdictCache(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Executor = "dagger"
	executor := &verdictCachingExecutor{}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}
	if result := runner.Execute(context.Background(), req); result.Status != "passed" {
		t.Fatal(result)
	}
	executor.failFresh = true
	req.Fresh = true
	if result := runner.Execute(context.Background(), req); result.Status != "failed" {
		t.Fatal(result)
	}
	req.Fresh = false
	result := runner.Execute(context.Background(), req)
	if result.Status != "failed" || result.Cache.Status != "fresh" {
		t.Fatalf("lower-level cached success hid known failure: %+v", result)
	}
}
