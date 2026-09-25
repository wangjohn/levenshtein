package verify

import (
	"context"
	"slices"
	"testing"
)

func TestDepsEnvDropsHostSettings(t *testing.T) {
	t.Parallel()
	env := depsEnv([]string{"PATH=/bin", "OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=/elsewhere", "HTTPS_PROXY=http://proxy"})
	if !slices.Equal(env, []string{"PATH=/bin", "HTTPS_PROXY=http://proxy"}) {
		t.Fatalf("unexpected osv-scanner environment: %v", env)
	}
}

// deps-vuln queries current advisory data, which no input fingerprint covers,
// so like go-vuln its verdict is never reused on either executor and its
// Dagger call is never answered from Dagger's own cache.
func TestDependencyScansNeverReuseAVerdict(t *testing.T) {
	t.Parallel()
	for _, kind := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		req := cacheRequest(t)
		req.Check.Kind = CheckDepsVuln
		req.Environment.Executor = kind
		executor := &countingExecutor{status: StatusPassed}
		runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

		for range 2 {
			result := runner.Execute(context.Background(), req)
			if result.Status != StatusPassed || result.Cache.Status != CacheDisabled || result.Cache.Reason == "" {
				t.Fatalf("%s: unexpected deps-vuln result: %+v", kind, result)
			}
		}
		if executor.calls != 2 {
			t.Fatalf("%s: reused a deps-vuln verdict: %d calls", kind, executor.calls)
		}
	}

	req := cacheRequest(t)
	req.Check.Kind = CheckDepsVuln
	if first, second := executionNonce(req), executionNonce(req); first == "" || first == second {
		t.Fatal("deps-vuln must get unique Dagger execution inputs")
	}
}
