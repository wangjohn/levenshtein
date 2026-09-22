package verify

import (
	"context"
	"strings"
	"testing"
)

func TestSharedChecksPlanFromVersionedConfiguration(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"},"vet":{"kind":"go-vet","target":"app","environment":"go"},"mod":{"kind":"go-mod","target":"app","environment":"go"},"test":{"kind":"go-test","target":"app","environment":"go"},"http":{"kind":"go-http","target":"app","environment":"go"},"sql":{"kind":"go-sql","target":"app","environment":"go"},"audit":{"kind":"go-vuln","target":"app","environment":"go"},"workflows":{"kind":"workflow-lint","target":"app","environment":"go"},"security":{"kind":"workflow-security","target":"app","environment":"go"}},"runs":{"custom":{"checks":["lint","vet","mod","test","http","sql","audit","workflows","security"]}}}`,
		`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"audit":{"kind":"go-vuln","target":"app","environment":"go"}},"runs":{"custom":{"checks":["audit"]}}}`,
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

	for range 2 {
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

// go mod verify inspects the module cache, which no input fingerprint covers,
// so a go-mod verdict is never reused on either executor and its Dagger call is
// never answered from Dagger's own cache.
func TestModuleChecksNeverReuseAVerdict(t *testing.T) {
	for _, kind := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		req := cacheRequest(t)
		req.Check.Kind = CheckGoMod
		req.Environment.Executor = kind
		executor := &countingExecutor{status: StatusPassed}
		runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

		for range 2 {
			result := runner.Execute(context.Background(), req)
			if result.Status != StatusPassed || result.Cache.Status != CacheDisabled || result.Cache.Reason == "" {
				t.Fatalf("%s: unexpected go-mod result: %+v", kind, result)
			}
		}
		if executor.calls != 2 {
			t.Fatalf("%s: reused a go-mod verdict: %d calls", kind, executor.calls)
		}
		executor.status = StatusFailed
		if result := runner.Execute(context.Background(), req); result.Status != StatusFailed {
			t.Fatalf("%s: cached success hid an untidy module: %+v", kind, result)
		}
	}

	req := cacheRequest(t)
	req.Check.Kind = CheckGoMod
	if first, second := executionNonce(req), executionNonce(req); first == "" || first == second {
		t.Fatal("go-mod must get unique Dagger execution inputs")
	}
}

// go-test's verdict is reused like go-vet's: tests that reach beyond their
// declared inputs belong in a command check. A failure is never cached, and a
// fresh run gets a nonce so Dagger re-executes it.
func TestTestResultsAreReusedUntilAFreshRun(t *testing.T) {
	for _, kind := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		req := cacheRequest(t)
		req.Check.Kind = CheckGoTest
		req.Environment.Executor = kind
		executor := &countingExecutor{status: StatusFailed}
		runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

		for range 2 {
			if result := runner.Execute(context.Background(), req); result.Status != StatusFailed {
				t.Fatalf("%s: unexpected go-test result: %+v", kind, result)
			}
		}
		executor.status = StatusPassed
		for range 2 {
			if result := runner.Execute(context.Background(), req); result.Status != StatusPassed {
				t.Fatalf("%s: unexpected go-test result: %+v", kind, result)
			}
		}
		if executor.calls != 3 {
			t.Fatalf("%s: want two failures run and one pass reused, got %d calls", kind, executor.calls)
		}
	}

	req := cacheRequest(t)
	req.Check.Kind = CheckGoTest
	if executionNonce(req) != "" {
		t.Fatal("an ordinary go-test run should keep Dagger's cache")
	}
	req.RerunChecks = true
	if executionNonce(req) == "" {
		t.Fatal("a fresh go-test run must get unique Dagger execution inputs")
	}
}

func TestWorkflowKindsRequireRootTarget(t *testing.T) {
	for _, kind := range []CheckKind{CheckWorkflowLint, CheckWorkflowSecurity} {
		cfg, err := Parse([]byte(`{"version":1,"targets":{"app":{"dir":"nested","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"workflow":{"kind":"` + string(kind) + `","target":"app","environment":"go"}},"runs":{"branch":{"checks":["workflow"]}}}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil || !strings.Contains(err.Error(), string(kind)+" requires a repository-root target") {
			t.Fatalf("%s accepted a non-root target: %v", kind, err)
		}
	}
}

func TestUnconfiguredRepoGetsSharedCheckDefaults(t *testing.T) {
	source := t.TempDir()
	cfg, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range map[string]int{"branch": 3, "pre-merge": 3, "main": 4, "go-lint": 1, "go-vet": 1, "go-mod": 1, "go-test": 1, "go-vuln": 1, "go-http": 1, "go-sql": 1, "workflow-lint": 1, "workflow-security": 1} {
		plan, err := cfg.Plan(source, name)
		if err != nil || len(plan.Checks) != count {
			t.Fatalf("%s: %+v %v", name, plan, err)
		}
		// Only the daily audit is fresh; every check is a Dagger check on the
		// whole-tree target and is named after its kind.
		if plan.RerunChecks != (name == "main") {
			t.Fatalf("%s: rerun_checks=%v", name, plan.RerunChecks)
		}
		for _, check := range plan.Checks {
			if check.ID != string(check.Check.Kind) || check.Environment.Executor != ExecutorDagger || check.Target.Dir != "." || len(check.Target.Inputs) != 1 || check.Target.Inputs[0] != "." {
				t.Fatalf("%s: unexpected default check %+v", name, check)
			}
		}
	}
}
