package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

// A shared checkout is pinned for the life of a run and its snapshot is
// memoized per root, so each side of the comparison uses its own checkout
// rather than editing one in place.
func TestReviewDaggerImplementationFilesAreInputs(t *testing.T) {
	for _, file := range []string{"runner/extra.go", "sdk/patched-go/src/patched_go/__init__.py"} {
		t.Run(file, func(t *testing.T) {
			req := cacheRequest(t)
			req.Environment.Executor = ExecutorDagger
			before, err := fingerprint(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}

			req.Shared = t.TempDir()
			path := filepath.Join(req.Shared, file)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("changed implementation"), 0644); err != nil {
				t.Fatal(err)
			}

			after, err := fingerprint(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if before == after {
				t.Fatal("Dagger runtime or SDK change did not invalidate verification")
			}
		})
	}
}

// The shared checkout is walked once per root and executor kind. Repeated
// fingerprints of one check must not re-read go.mod, cmd, internal and the
// Dagger runtime, and the memo must not leak between separate checkouts.
func TestSharedImplementationIsSnapshotOncePerCheckout(t *testing.T) {
	req := cacheRequest(t)
	req.Environment.Executor = ExecutorDagger
	before, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	// An edit to the pinned checkout during a run is deliberately invisible.
	if err := os.WriteFile(filepath.Join(req.Shared, "go.mod"), []byte("module later\n"), 0644); err != nil {
		t.Fatal(err)
	}
	again, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if again != before {
		t.Fatal("shared checkout was walked again within one run")
	}

	// A different checkout with that same content still gets its own snapshot.
	req.Shared = t.TempDir()
	if err := os.WriteFile(filepath.Join(req.Shared, "go.mod"), []byte("module later\n"), 0644); err != nil {
		t.Fatal(err)
	}
	other, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if other == before {
		t.Fatal("memoized snapshot leaked across shared checkouts")
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
	waits := waitingFor(t)
	done := make(chan Result, 1)
	go func() { done <- runner.Execute(context.Background(), req) }()
	awaitWait(t, waits, "result-"+first.Cache.Key)
	if err := os.WriteFile(filepath.Join(req.Source, "input"), []byte("changed while waiting"), 0600); err != nil {
		t.Fatal(err)
	}
	unlock()
	got := <-done
	if got.Cache.Status == CacheHit {
		t.Fatalf("returned old cached pass after source changed during lock wait: %+v", got)
	}
}

// A failing check can leave untracked files in another target's inputs. The
// run's memoized git listing has to forget them as surely as it does after a
// pass, or the other target's next lookup hits a key that cannot see them.
func TestFailedCheckFilesAreSeenByLaterChecks(t *testing.T) {
	requireGit(t)
	other := cacheRequest(t)
	runGit(t, other.Source, "init")
	writeFile(t, filepath.Join(other.Source, "lib", "tracked.go"), "package lib\n")
	other.Target.Inputs = []string{"lib"}
	other.Target.Discovery = DiscoveryGit
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: &countingExecutor{status: StatusPassed}}
	if result := runner.Execute(t.Context(), other); result.Cache.Status != CacheMiss {
		t.Fatalf("cold: %+v", result.Cache)
	}

	failing := cacheRequest(t)
	failing.Source = other.Source
	failing.ID = "writer"
	failing.Check.Command.Cache = false
	writer := CachedExecutor{Cache: runner.Cache, Executor: &countingExecutor{status: StatusFailed, artifact: "lib/generated.go"}}
	if result := writer.Execute(t.Context(), failing); result.Status != StatusFailed {
		t.Fatalf("writer: %+v", result)
	}

	if result := runner.Execute(t.Context(), other); result.Cache.Status == CacheHit {
		t.Fatalf("a later check reused a key that could not see a file a failed check created: %+v", result.Cache)
	}
}

// A preparation stage can create files in its build's inputs, and the build's
// key is taken right after it, so the stage has to refresh the listing itself.
// Otherwise the build is recorded under a key that omits the generated file,
// and a later run without that file reuses a build that was made with it.
func TestBuildKeySeesFilesItsPreparationCreated(t *testing.T) {
	requireGit(t)
	req := nativeRequest(t)
	runGit(t, req.Source, "init")
	writeFile(t, filepath.Join(req.Source, "gen", "keep"), "")
	writeFile(t, filepath.Join(req.Source, "lock"), "v1")
	req.Target.Discovery = DiscoveryGit
	req.Environment.Identity = "generated-inputs-fixture"
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "echo x > gen/new; touch ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	req.Build = &Preparation{Command: []string{"/bin/sh", "-c", "ls gen > out"}, Inputs: []string{"gen"}, Outputs: []string{"out"}}
	req.Check.Command.Args = []string{"/bin/sh", "-c", "cat out"}
	native := &Native{Cache: &Cache{Dir: t.TempDir()}}
	if result := native.Execute(t.Context(), req); result.Status != StatusPassed || result.Stdout != "keep\nnew\n" {
		t.Fatalf("first build: %+v", result)
	}

	// A later run lists the work tree afresh and finds the generated file gone,
	// while the preparation's own inputs are unchanged, so it is reused.
	relist(req.Source)
	if err := os.Remove(filepath.Join(req.Source, "gen", "new")); err != nil {
		t.Fatal(err)
	}

	result := native.Execute(t.Context(), req)
	if result.Status != StatusPassed || result.Stdout != "keep\n" {
		t.Fatalf("a build made with a generated input was reused without it: %+v", result)
	}
}
