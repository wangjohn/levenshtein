package verify

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCachedResultIdentity(t *testing.T) {
	t.Parallel()
	req := cacheRequest(t)
	cache := &Cache{Dir: t.TempDir()}
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: cache, Executor: executor}

	first := runner.Execute(t.Context(), req)
	second := runner.Execute(t.Context(), req)
	want, err := implementation(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Implementation == nil || first.Implementation.Digest != want || second.Cache.Status != CacheHit || second.Implementation == nil || *second.Implementation != *first.Implementation || executor.calls != 1 {
		t.Fatalf("identity lost on hit: first=%+v second=%+v", first, second)
	}

	report := sessionOf(req).Execute(t.Context(), Plan{Source: req.Source, Checks: []PlannedCheck{req.PlannedCheck}}, req.Shared, map[ExecutorKind]Executor{ExecutorNative: runner}, 1)
	if report.Version != 1 || report.Results[0].Implementation == nil || report.Results[0].Implementation.Digest != want {
		t.Fatalf("report identity: %+v", report)
	}
}

func TestToolchainReportDoesNotExposeSettings(t *testing.T) {
	t.Parallel()
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckGoVet}
	req.Environment.Env = map[string]string{"GOFLAGS": "-ldflags=-X=token=secret-value"}

	_, safe := resultIdentity(t.Context(), req)
	if safe == nil {
		t.Fatal("toolchain identity unavailable")
	}
	want, err := hostToolchain(t.Context(), req, nativeEnv(req, req.Check.env()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-value") || strings.Contains(string(data), "GOFLAGS") || safe.Digest != want || safe.SettingsDigest == "" {
		t.Fatalf("unsafe or inconsistent identity: %s", data)
	}

	var old Report
	if err := json.Unmarshal([]byte(`{"version":1,"run":"branch","status":"passed","results":[]}`), &old); err != nil || old.Version != 1 || old.Build != nil {
		t.Fatalf("old report: %+v %v", old, err)
	}
}
