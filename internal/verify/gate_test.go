package verify

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitingFor installs the lock-wait hook for one test and returns a channel
// that receives every lock name a caller starts waiting on.
func waitingFor(t *testing.T) <-chan string {
	t.Helper()
	waits := make(chan string, 16)
	lockWaiting = func(name string) {
		select {
		case waits <- name:
		default:
		}
	}
	t.Cleanup(func() { lockWaiting = nil })
	return waits
}

// awaitWait blocks until a caller waits on a lock whose name contains want.
func awaitWait(t *testing.T, waits <-chan string, want string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case name := <-waits:
			if strings.Contains(name, want) {
				return
			}
		case <-deadline:
			t.Fatalf("nothing waited on a %q lock", want)
		}
	}
}

// rendezvousExecutor passes only when every check it runs is inside Execute at
// the same time, which serialized checks can never be.
type rendezvousExecutor struct {
	arrived sync.WaitGroup
	all     chan struct{}
	once    sync.Once
}

func (e *rendezvousExecutor) Execute(ctx context.Context, req Request) Result {
	e.arrived.Done()
	go e.once.Do(func() {
		e.arrived.Wait()
		close(e.all)
	})

	select {
	case <-e.all:
		return Result{Status: StatusPassed}
	case <-time.After(10 * time.Second):
		return Result{Status: StatusFailed, Error: "checks did not overlap"}
	}
}

// Read-only native checks share the working tree, so one run executes them
// side by side instead of queueing them behind a single workspace lock.
func TestReadOnlyNativeChecksRunConcurrently(t *testing.T) {
	req := nativeRequest(t)
	var checks []PlannedCheck
	for _, id := range []string{"vet-a", "vet-b"} {
		checks = append(checks, PlannedCheck{
			ID:          id,
			Check:       Check{Kind: CheckGoVet, Target: id, Environment: "host"},
			Target:      req.Target,
			Environment: req.Environment,
		})
	}
	executor := &rendezvousExecutor{all: make(chan struct{})}
	executor.arrived.Add(len(checks))
	cached := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

	report := Execute(t.Context(), Plan{Source: req.Source, Checks: checks}, req.Shared, map[ExecutorKind]Executor{ExecutorNative: cached}, 4)
	for _, result := range report.Results {
		if result.Status != StatusPassed {
			t.Fatalf("read-only native checks were serialized: %+v", result)
		}
	}
}

// A check that writes the working tree still excludes every other native
// check, in this process and in another one holding the workspace file lock.
func TestCommandChecksExcludeOtherNativeChecks(t *testing.T) {
	req := cacheRequest(t)
	cache := &Cache{Dir: t.TempDir()}
	waits := waitingFor(t)
	unlock, err := lockFile(t.Context(), filepath.Join(cache.Dir, "locks", "workspace-"+digest(req.Source)))
	if err != nil {
		t.Fatal(err)
	}
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: cache, Executor: executor}

	done := make(chan Result, 1)
	go func() { done <- runner.Execute(t.Context(), req) }()
	awaitWait(t, waits, "workspace-")
	select {
	case result := <-done:
		t.Fatalf("a native check ran while another process held the workspace: %+v", result)
	default:
	}
	unlock()
	if result := <-done; result.Status != StatusPassed || executor.calls != 1 {
		t.Fatalf("the check did not run once the workspace was free: %+v", result)
	}

	release, err := acquireWorkspace(t.Context(), cache.Dir, req.Source, true)
	if err != nil {
		t.Fatal(err)
	}
	vet := req
	vet.Check = Check{Kind: CheckGoVet, Target: "app", Environment: "host"}
	go func() { done <- runner.Execute(t.Context(), vet) }()
	awaitWait(t, waits, "gate:")
	release()
	if result := <-done; result.Status != StatusPassed {
		t.Fatalf("a read-only check did not run after the writer finished: %+v", result)
	}
}

// Cancelling a check that is still waiting for its workspace reports it as
// cancelled, wherever the wait happens.
func TestCancelWhileWaitingForTheWorkspaceIsCancelled(t *testing.T) {
	for _, tc := range []struct {
		name string
		hold func(t *testing.T, cache *Cache, source string) func()
		wait string
	}{
		{
			name: "in-process writer",
			hold: func(t *testing.T, cache *Cache, source string) func() {
				t.Helper()
				release, err := acquireWorkspace(t.Context(), cache.Dir, source, true)
				if err != nil {
					t.Fatal(err)
				}
				return release
			},
			wait: "gate:",
		},
		{
			name: "another process",
			hold: func(t *testing.T, cache *Cache, source string) func() {
				t.Helper()
				unlock, err := lockFile(t.Context(), filepath.Join(cache.Dir, "locks", "workspace-"+digest(source)))
				if err != nil {
					t.Fatal(err)
				}
				return unlock
			},
			wait: "workspace-",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := cacheRequest(t)
			req.Check.Command.Cache = false
			cache := &Cache{Dir: t.TempDir()}
			waits := waitingFor(t)
			release := tc.hold(t, cache, req.Source)
			defer release()
			executor := &countingExecutor{status: StatusPassed}
			ctx, cancel := context.WithCancel(t.Context())

			done := make(chan Result, 1)
			go func() { done <- (CachedExecutor{Cache: cache, Executor: executor}).Execute(ctx, req) }()
			awaitWait(t, waits, tc.wait)
			cancel()

			if result := <-done; result.Status != StatusCancelled || executor.calls != 0 {
				t.Fatalf("a check cancelled while waiting for its workspace: %+v after %d calls", result, executor.calls)
			}
		})
	}
}

// A hit with no artifacts to restore reads only the cache, so it does not wait
// for a check that holds the working tree; restoring artifacts writes the tree,
// so that hit does.
func TestCacheHitsEnterTheWorkspaceOnlyToRestoreArtifacts(t *testing.T) {
	req := cacheRequest(t)
	cache := &Cache{Dir: t.TempDir()}
	executor := &countingExecutor{status: StatusPassed, artifact: "report.txt"}
	runner := CachedExecutor{Cache: cache, Executor: executor}
	restoring := *req.Check.Command
	restoring.Artifacts = []string{"report.txt"}
	withArtifact := req
	withArtifact.Check.Command = &restoring
	runner.Execute(t.Context(), req)
	runner.Execute(t.Context(), withArtifact)
	locked, err := lockFile(t.Context(), filepath.Join(cache.Dir, "locks", "workspace-"+digest(req.Source)))
	if err != nil {
		t.Fatal(err)
	}
	unlock := sync.OnceFunc(locked)
	defer unlock()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if result := runner.Execute(ctx, req); result.Cache.Status != CacheHit {
		t.Fatalf("a hit with nothing to restore waited for the workspace: %+v", result)
	}

	waits := waitingFor(t)
	done := make(chan Result, 1)
	go func() { done <- runner.Execute(t.Context(), withArtifact) }()
	awaitWait(t, waits, "workspace-")
	unlock()
	if result := <-done; result.Cache.Status != CacheHit || executor.calls != 2 {
		t.Fatalf("an artifact restore did not wait for the workspace and then hit: %+v", result)
	}
}
