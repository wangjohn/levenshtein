package verify

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// sessionAt opens a session whose stat memo persists under dir, or nowhere
// when dir is empty.
func sessionAt(dir string) *Session {
	return newSession(dir, recordLimit)
}

// Everything a session remembers lasts for that session only. A later session
// in the same process, as a watch loop or an editor integration would open,
// sees the source, its listing, and the shared checkout as they are then.
func TestALaterSessionSeesWhatAnEarlierOneRemembered(t *testing.T) {
	t.Parallel()
	requireGit(t)
	req := cacheRequest(t)
	runGit(t, req.Source, "init")
	req.Target.Inputs = []string{"."}
	req.Target.Discovery = DiscoveryGit
	req.Environment.Executor = ExecutorDagger
	first := sessionAt("")
	req.session = first
	before, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(req.Shared, "go.mod"), "module later\n")
	writeFile(t, filepath.Join(req.Source, "new.go"), sourceOne)
	listed, _ := first.gitFiles(t.Context(), req.Source)
	if again, err := fingerprint(t.Context(), req); err != nil || again != before || slices.Contains(listed, "new.go") {
		t.Fatalf("one session changed its answers mid-run: %v, %v", err, listed)
	}

	req.session = sessionAt("")
	after, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	listed, _ = req.session.gitFiles(t.Context(), req.Source)
	if after == before || !slices.Contains(listed, "new.go") {
		t.Fatalf("a new session reused an earlier session's answers: %v", listed)
	}
	req.session = first
	if again, err := fingerprint(t.Context(), req); err != nil || again != before {
		t.Fatalf("a new session disturbed the earlier one: %v", err)
	}
}

// The workspace gate stays one per process per source, whichever session asks,
// so two runs in one process still keep a writer alone in the tree.
func TestSessionsInOneProcessShareTheWorkspaceGate(t *testing.T) {
	t.Parallel()
	req := cacheRequest(t)
	release, err := sessionAt("").acquireWorkspace(t.Context(), "", req.Source, true)
	if err != nil {
		t.Fatal(err)
	}
	waits := waitingFor(t, &req)

	done := make(chan error, 1)
	go func() {
		leave, err := req.session.acquireWorkspace(t.Context(), "", req.Source, false)
		if err == nil {
			leave()
		}
		done <- err
	}()
	awaitWait(t, waits, "gate:")
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Separate registries stand for separate processes: they share nothing in
// memory, so only the cache directory's file lock keeps a writer in one from
// entering a tree a reader in the other is inside.
func TestSeparateWorkspacesMeetAtTheFileLock(t *testing.T) {
	t.Parallel()
	req := cacheRequest(t)
	dir := t.TempDir()
	release, err := (&workspaces{}).acquire(t.Context(), dir, req.Source, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	waits := waitingFor(t, &req)
	req.session.workspaces = &workspaces{}

	done := make(chan error, 1)
	go func() {
		leave, err := req.session.acquireWorkspace(t.Context(), dir, req.Source, true)
		if err == nil {
			leave()
		}
		done <- err
	}()
	awaitWait(t, waits, "workspace-")
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Flush persists what the session hashed under the cache it was opened with.
func TestNewSessionPersistsItsStatMemoUnderTheCache(t *testing.T) {
	t.Parallel()
	cache := &Cache{Dir: t.TempDir()}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "input.go"), sourceOne)
	session := NewSession(cache)
	mustSnapshot(t, session, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem})

	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache.Dir, "stat", digest(root)+".json")); err != nil {
		t.Fatalf("the session's stat memo was not persisted under its cache: %v", err)
	}
	if err := NewSession(nil).Flush(); err != nil {
		t.Fatalf("a session without a cache has nothing to persist: %v", err)
	}
}
