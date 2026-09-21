package verify

import (
	"context"
	"testing"
)

func TestSharedChecksPlanFromVersionedConfiguration(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"},"vet":{"kind":"go-vet","target":"app","environment":"go"},"http":{"kind":"go-http","target":"app","environment":"go"},"sql":{"kind":"go-sql","target":"app","environment":"go"},"audit":{"kind":"go-vuln","target":"app","environment":"go"},"workflows":{"kind":"workflow-lint","target":"app","environment":"go"}},"runs":{"custom":{"checks":["lint","vet","http","sql","audit","workflows"]}}}`,
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

func TestUnconfiguredRepoGetsSharedCheckDefaults(t *testing.T) {
	source := t.TempDir()
	cfg, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range map[string]int{"branch": 2, "pre-merge": 2, "main": 3, "go-vuln": 1, "go-http": 1, "go-sql": 1, "workflow-lint": 1} {
		plan, err := cfg.Plan(source, name)
		if err != nil || len(plan.Checks) != count {
			t.Fatalf("%s: %+v %v", name, plan, err)
		}
	}
}
