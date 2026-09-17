package verify

import (
	"context"
	"testing"
)

func TestSharedChecksPlanFromBothConfigFormats(t *testing.T) {
	for _, data := range []string{
		`{"modules":["."],"runs":{"custom":["go-lint","go-vet","go-http","go-sql","go-vuln","workflow-lint"]}}`,
		`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"audit":{"kind":"go-vuln","target":"app","environment":"go","cache":true}},"runs":{"custom":{"checks":["audit"]}}}`,
	} {
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		plan, err := cfg.Plan(t.TempDir(), "custom")
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range plan.Checks {
			if daggerFunctions[check.Check.Kind] == "" {
				t.Fatalf("unsupported plan: %+v", check)
			}
		}
	}
}

func TestVulnerabilityResultsNeverReuseSourceOnlyCache(t *testing.T) {
	req := cacheRequest(t)
	req.Check.Kind = CheckGoVuln
	req.Environment.Executor = ExecutorDagger
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

	for i := 0; i < 2; i++ {
		result := runner.Execute(context.Background(), req)
		if result.Status != StatusPassed || result.Cache.Status != CacheDisabled {
			t.Fatalf("unexpected audit: %+v", result)
		}
	}
	if executor.calls != 2 {
		t.Fatalf("reused stale verdict: %d calls", executor.calls)
	}
	executor.status = StatusFailed
	if result := runner.Execute(context.Background(), req); result.Status != StatusFailed {
		t.Fatalf("cached success hid a new vulnerability: %+v", result)
	}
	executor.status = StatusError
	if result := runner.Execute(context.Background(), req); result.Status != StatusError {
		t.Fatalf("cached success hid a scanner outage: %+v", result)
	}

	first, second := executionNonce(req), executionNonce(req)
	if first == "" || first == second {
		t.Fatal("audit must get unique Dagger execution inputs")
	}
	req.Check.Kind = CheckGoLint
	if executionNonce(req) != "" {
		t.Fatal("ordinary lint should preserve Dagger cache")
	}
}

func TestWorkflowLintRequiresRootTarget(t *testing.T) {
	cfg, err := Parse([]byte(`{"version":1,"targets":{"app":{"dir":"nested","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"workflow":{"kind":"workflow-lint","target":"app","environment":"go"}},"runs":{"branch":{"checks":["workflow"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil {
		t.Fatal("accepted non-root workflow target")
	}
}
