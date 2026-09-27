package verify

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wangjohn/levenshtein/internal/testgit"
)

// Every line is twelve bytes, so an edit can keep a file's size and prove the
// memo is answering from the recorded stat rather than from content.
const (
	sourceOne   = "package one\n"
	sourceTwo   = "package two\n"
	sourceThree = "package six\n"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func setModTime(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func mustSnapshot(t *testing.T, session *Session, req snapshotRequest) string {
	t.Helper()
	value, err := session.snapshot(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// The memo trades a content read for a stat comparison, so an edit that
// preserves every stat field is invisible within a process, and a change to any
// one of size, modification time or inode is not.
func TestFileStatMemoReusesOnlyIdenticalStats(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "input.go")
	writeFile(t, path, sourceOne)
	// Settled: modified well before it is hashed, so the memo may trust it.
	stamp := modTime(t, path).Add(-time.Minute)
	setModTime(t, path, stamp)
	session := sessionAt("")
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, session, req)

	writeFile(t, path, sourceTwo)
	setModTime(t, path, stamp)
	if reused := mustSnapshot(t, session, req); reused != before {
		t.Fatal("identical stat did not reuse the memoized hash")
	}

	// Modification time: the memo now holds the first content for a stat that no
	// longer matches, so the second content has to be read.
	setModTime(t, path, stamp.Add(time.Second))
	edited := mustSnapshot(t, session, req)
	if edited == before {
		t.Fatal("changed modification time reused the memoized hash")
	}

	// Inode: same size, same modification time, same mode, different file.
	replacement := filepath.Join(root, "replacement")
	writeFile(t, replacement, sourceThree)
	setModTime(t, replacement, stamp.Add(time.Second))
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	replaced := mustSnapshot(t, session, req)
	if replaced == edited {
		t.Fatal("replaced file reused the memoized hash")
	}

	// Size.
	writeFile(t, path, sourceThree+"\n")
	setModTime(t, path, stamp.Add(time.Second))
	if grown := mustSnapshot(t, session, req); grown == replaced {
		t.Fatal("changed size reused the memoized hash")
	}
}

// A file hashed in the tick it was written can be rewritten, same size, in
// that tick, leaving every stat field unchanged; filesystems with coarse
// timestamps, such as Dagger's, do this routinely. The in-memory memo must
// reread such a file rather than trust the stat.
func TestFileStatMemoRereadsARacyFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "input.go")
	writeFile(t, path, sourceOne)
	stamp := modTime(t, path)
	session := sessionAt("")
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, session, req)
	writeFile(t, path, sourceTwo)
	setModTime(t, path, stamp)
	after := mustSnapshot(t, session, req)

	if after == before {
		t.Fatal("a same-tick rewrite of a freshly hashed file reused the stale hash")
	}
}

// A persisted entry is a hint the next process revalidates. It is only usable
// when the record was written well after the file settled; git's racy-index
// window is what decides that.
func TestPersistedStatCacheHonorsTheRacyWindow(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	settled := t.TempDir()
	settledPath := filepath.Join(settled, "input.go")
	old := time.Now().Add(-time.Hour)
	writeFile(t, settledPath, sourceOne)
	setModTime(t, settledPath, old)
	session := sessionAt(cacheDir)
	req := snapshotRequest{Root: settled, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, session, req)
	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}

	// A new session reads the persisted record, the way a second CLI process
	// would.
	session = sessionAt(cacheDir)
	writeFile(t, settledPath, sourceTwo)
	setModTime(t, settledPath, old)
	if reused := mustSnapshot(t, session, req); reused != before {
		t.Fatal("a settled persisted entry was not reused")
	}

	// A plain touch is not an edit: the stat no longer matches, so the file is
	// read again and yields the same content hash.
	touched := t.TempDir()
	touchedPath := filepath.Join(touched, "input.go")
	writeFile(t, touchedPath, sourceOne)
	setModTime(t, touchedPath, old)
	touchedReq := snapshotRequest{Root: touched, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	touchedBefore := mustSnapshot(t, session, touchedReq)
	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}

	// A new session reads the persisted record, the way a second CLI process
	// would.
	session = sessionAt(cacheDir)
	setModTime(t, touchedPath, time.Now())
	if after := mustSnapshot(t, session, touchedReq); after != touchedBefore {
		t.Fatal("touching a file changed the digest")
	}

	// The same size-preserving edit against a record written while the file was
	// still racy must be detected instead.
	racy := t.TempDir()
	racyPath := filepath.Join(racy, "input.go")
	writeFile(t, racyPath, sourceOne)
	stamp := modTime(t, racyPath)
	racyReq := snapshotRequest{Root: racy, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	racyBefore := mustSnapshot(t, session, racyReq)
	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}

	// A new session reads the persisted record, the way a second CLI process
	// would.
	session = sessionAt(cacheDir)
	writeFile(t, racyPath, sourceTwo)
	setModTime(t, racyPath, stamp)
	if hidden := mustSnapshot(t, session, racyReq); hidden == racyBefore {
		t.Fatal("a racy persisted entry hid a same-size edit")
	}
}

func TestUnreadableStatRecordIsNotFatal(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "input.go"), sourceOne)
	session := sessionAt(cacheDir)
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	want := mustSnapshot(t, session, req)
	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "stat", digest(root)+".json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}

	// A new session reads the persisted record, the way a second CLI process
	// would.
	session = sessionAt(cacheDir)
	if got := mustSnapshot(t, session, req); got != want {
		t.Fatalf("corrupt record changed the digest: %q want %q", got, want)
	}
}

// runGit runs git in root with a throwaway HOME, so the developer's own
// configuration cannot shape a fixture.
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := testgit.Command(t.Context(), "git", root, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
}

func gitRepository(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, ".gitignore"), "generated/\n")
	writeFile(t, filepath.Join(root, "tracked.go"), sourceOne)
	writeFile(t, filepath.Join(root, "removed.go"), sourceOne)
	writeFile(t, filepath.Join(root, "untracked.go"), sourceOne)
	writeFile(t, filepath.Join(root, "generated", "client.go"), sourceOne)
	runGit(t, root, "add", ".gitignore", "tracked.go", "removed.go")
	if err := os.Remove(filepath.Join(root, "removed.go")); err != nil {
		t.Fatal(err)
	}
	return root
}

// Git discovery hashes what the work tree knows about: tracked and untracked
// files, never ignored ones, and never a tracked path that is not on disk. A
// submodule is listed as a single gitlink, so its contents are walked. Leaving
// ignored files out is sound only because the executors leave them out too
// (TestFileSetConformance); the Go kinds, whose toolchain can load ignored
// files, add those it can (TestGoKindKeySkipsUnloadableIgnoredTrees).
func TestGitDiscoveryFollowsTheWorkTree(t *testing.T) {
	t.Parallel()
	root := gitRepository(t)
	runGit(t, root, "update-index", "--add", "--cacheinfo", "160000,0123456789abcdef0123456789abcdef01234567,sub")
	writeFile(t, filepath.Join(root, "sub", "lib.go"), sourceOne)
	session := sessionAt("")
	git := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryGit}
	host := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, session, git)
	if before == mustSnapshot(t, session, host) {
		t.Fatal("git and filesystem discovery cannot agree while an ignored file exists")
	}

	writeFile(t, filepath.Join(root, "generated", "client.go"), sourceTwo+sourceTwo)
	if ignored := mustSnapshot(t, session, git); ignored != before {
		t.Fatal("an ignored file entered the fingerprint")
	}
	if watched := mustSnapshot(t, session, host); watched == before {
		t.Fatal("filesystem discovery lost the ignored file")
	}

	for _, name := range []string{"tracked.go", "untracked.go", "sub/lib.go"} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), sourceTwo+sourceTwo)
		changed := mustSnapshot(t, session, git)
		if changed == before {
			t.Fatalf("editing %s did not change the fingerprint", name)
		}
		before = changed
	}

	// A tracked file that is not on disk is absent rather than "missing", and no
	// directory has an entry of its own except the walked submodule, so the same
	// content in a fresh clone fingerprints the same way.
	entries, err := session.snapshotEntries(t.Context(), git)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, value := range entries {
		if value == "directory" && name != "sub" || value == "missing" {
			t.Fatalf("git discovery recorded %s as %s", name, value)
		}
		names = append(names, filepath.ToSlash(name))
	}
	slices.Sort(names)
	if want := []string{".gitignore", "sub", "sub/lib.go", "tracked.go", "untracked.go"}; !slices.Equal(names, want) {
		t.Fatalf("git discovery enumerated %v, want %v", names, want)
	}
}

// A declared input that exists nowhere is still recorded as missing, so adding
// it later invalidates the cache exactly as the filesystem walk does. A declared
// input the work tree ignores exists, and so contributes nothing at all: no
// executor under git discovery reads it either.
func TestGitDiscoveryRecordsMissingButNotIgnoredInputs(t *testing.T) {
	t.Parallel()
	root := gitRepository(t)
	session := sessionAt("")

	for _, tt := range []struct {
		path string
		want map[string]string
	}{{path: "optional.go", want: map[string]string{"optional.go": "missing"}}, {path: "generated", want: map[string]string{}}, {path: "removed.go", want: map[string]string{}}} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			entries, err := session.snapshotEntries(t.Context(), snapshotRequest{Root: root, Paths: []string{tt.path}, Discovery: DiscoveryGit})
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(entries, tt.want) {
				t.Fatalf("entries = %v, want %v", entries, tt.want)
			}
		})
	}
}

// Outside a work tree git discovery is not an error, it is just unavailable.
func TestGitDiscoveryFallsBackOutsideAWorkTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "input.go"), sourceOne)
	session := sessionAt("")

	git := mustSnapshot(t, session, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryGit})
	host := mustSnapshot(t, session, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem})
	if git != host {
		t.Fatal("a directory that is not a work tree must fall back to the filesystem walk")
	}
	if note := session.discoveryNote(t.Context(), root, DiscoveryGit); note != "" {
		t.Fatalf("ordinary fallback reported a problem: %q", note)
	}
}

// A lookup whose context has ended cannot run git, which says nothing about the
// work tree. The next lookup must ask git again rather than inherit a
// filesystem walk for the rest of the run.
func TestGitDiscoveryDoesNotMemoizeACancelledListing(t *testing.T) {
	t.Parallel()
	root := gitRepository(t)
	session := sessionAt("")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	if _, ok := session.gitFiles(cancelled, root); ok {
		t.Fatal("a cancelled lookup listed files")
	}
	if note := session.discoveryNote(cancelled, root, DiscoveryGit); !strings.Contains(note, "git input discovery failed") {
		t.Fatalf("a cancelled lookup gave no reason: %q", note)
	}

	listed, ok := session.gitFiles(t.Context(), root)
	if !ok || !slices.Contains(listed, "tracked.go") {
		t.Fatalf("the cancelled lookup was memoized: %v, %v", listed, ok)
	}
	if note := session.discoveryNote(t.Context(), root, DiscoveryGit); note != "" {
		t.Fatalf("the cancelled lookup's reason outlived it: %q", note)
	}
}

// A listing that git produced before a check created files, but that finishes
// after the check relisted, would put the stale view back for every later key
// in the run. It serves its own caller and is not memoized.
func TestGitDiscoveryDoesNotMemoizeAListingARelistOvertook(t *testing.T) {
	t.Parallel()
	root := gitRepository(t)
	session := newSession("", recordLimit)
	session.listingFetched = func(source string) {
		session.listingFetched = nil
		writeFile(t, filepath.Join(source, "created.go"), sourceOne)
		session.relist(source)
	}

	if listed, _ := session.gitFiles(t.Context(), root); slices.Contains(listed, "created.go") {
		t.Fatal("the overtaken listing saw a file created after git listed")
	}

	listed, ok := session.gitFiles(t.Context(), root)
	if !ok || !slices.Contains(listed, "created.go") {
		t.Fatalf("a listing overtaken by relist was memoized: %v, %v", listed, ok)
	}
}

func TestTargetExcludeLeavesPathsOutOfTheFingerprint(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"), sourceOne)
	writeFile(t, filepath.Join(root, "build", "out.bin"), sourceOne)
	session := sessionAt("")
	req := snapshotRequest{Root: root, Paths: []string{"."}, Excludes: []string{"build"}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, session, req)

	writeFile(t, filepath.Join(root, "build", "out.bin"), sourceTwo+sourceTwo)
	if excluded := mustSnapshot(t, session, req); excluded != before {
		t.Fatal("an excluded path entered the fingerprint")
	}
	writeFile(t, filepath.Join(root, "src", "main.go"), sourceTwo+sourceTwo)
	if included := mustSnapshot(t, session, req); included == before {
		t.Fatal("exclude swallowed a declared input")
	}

	want := []string{"**/.env", "**/.env.*", "!**/.env.example", "**/.git", "build", "build/**"}
	got := daggerExcludes([]string{"build"})
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Dagger excludes = %v, want %v", got, want)
	}
}

// Every kind keeps the target's discovery, the Go kinds included: a Go kind
// widens git discovery itself with the ignored paths its toolchain can load.
func TestPlanValidatesDiscoveryAndExclude(t *testing.T) {
	t.Parallel()
	const config = `{"version":1,"targets":{"app":{"dir":".","inputs":["."]%s}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":%q,"target":"app","environment":"go"}},"runs":{"branch":{"checks":["lint"]}}}`
	for _, tt := range []struct {
		name   string
		kind   CheckKind
		target string
		want   DiscoveryKind
	}{
		{name: "default", kind: CheckSecrets, want: DiscoveryGit},
		{name: "explicit filesystem", kind: CheckSecrets, target: `,"discovery":"filesystem"`, want: DiscoveryFilesystem},
		{name: "explicit git", kind: CheckSecrets, target: `,"discovery":"git","exclude":["node_modules","build"]`, want: DiscoveryGit},
		{name: "Go kind by default", kind: CheckGoLint, want: DiscoveryGit},
		{name: "Go kind with explicit filesystem", kind: CheckGoTest, target: `,"discovery":"filesystem"`, want: DiscoveryFilesystem},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Parse([]byte(fmt.Sprintf(config, tt.target, tt.kind)))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := cfg.Plan(t.TempDir(), "branch")
			if err != nil {
				t.Fatal(err)
			}
			if plan.Checks[0].Target.Discovery != tt.want {
				t.Fatalf("discovery = %q, want %q", plan.Checks[0].Target.Discovery, tt.want)
			}
		})
	}

	for _, target := range []string{`,"discovery":"svn"`, `,"exclude":["build/**"]`, `,"exclude":["../outside"]`, `,"exclude":["."]`} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			cfg, err := Parse([]byte(fmt.Sprintf(config, target, CheckGoLint)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil {
				t.Fatal("accepted an invalid target")
			}
		})
	}
}

// Only the repository's own .gitignore files decide what is ignored. A personal
// excludes file, a configured core.excludesFile and .git/info/exclude would each
// make one developer's fingerprint differ from a colleague's.
func TestGitDiscoveryIgnoresPersonalExcludes(t *testing.T) {
	root := gitRepository(t)
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "git", "ignore"), "personal.go\n")
	writeFile(t, filepath.Join(home, "excludes"), "configured.go\n")
	writeFile(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesFile = "+filepath.Join(home, "excludes")+"\n")
	writeFile(t, filepath.Join(root, ".git", "info", "exclude"), "local.go\n")
	for _, name := range []string{"personal.go", "configured.go", "local.go"} {
		writeFile(t, filepath.Join(root, name), sourceOne)
	}
	t.Setenv("HOME", home)

	listed, ok := sessionAt("").gitFiles(t.Context(), root)
	if !ok {
		t.Fatal("git listing unavailable inside a work tree")
	}
	for _, name := range []string{"personal.go", "configured.go", "local.go"} {
		if !slices.Contains(listed, name) {
			t.Errorf("a personal ignore rule hid %s: %v", name, listed)
		}
	}
	if slices.Contains(listed, filepath.Join("generated", "client.go")) {
		t.Fatal("the repository's own .gitignore stopped applying")
	}
}

// A relative PATH entry resolves inside the repository being verified, so
// discovery must never run a git found there.
func TestGitDiscoveryIgnoresRelativePathEntries(t *testing.T) {
	root := gitRepository(t)
	writeFile(t, filepath.Join(root, "evil", "git"), "#!/bin/sh\ntouch \"$PWD/pwned\"\n")
	if err := os.Chmod(filepath.Join(root, "evil", "git"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "evil"+string(filepath.ListSeparator)+"."+string(filepath.ListSeparator)+os.Getenv("PATH"))

	found, ok := hostGit()
	if !ok || !filepath.IsAbs(found) {
		t.Fatalf("host git = %q, %v", found, ok)
	}
	if _, ok := sessionAt("").gitFiles(t.Context(), root); !ok {
		t.Fatal("git listing unavailable inside a work tree")
	}
	if _, err := os.Stat(filepath.Join(root, "pwned")); !os.IsNotExist(err) {
		t.Fatalf("discovery ran the repository's own git: %v", err)
	}
}

// The listing is memoized per run, but a check can create inputs. The
// post-execution fingerprint must see them, so the result is not cached under
// a key that predates them.
func TestFilesCreatedByACheckAreSeenAfterExecution(t *testing.T) {
	t.Parallel()
	requireGit(t)
	req := cacheRequest(t)
	req.session = sessionAt("")
	runGit(t, req.Source, "init")
	req.Target.Inputs = []string{"."}
	req.Target.Discovery = DiscoveryGit
	executor := &countingExecutor{status: StatusPassed, artifact: "created.go"}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

	result := runner.Execute(context.Background(), req)
	if result.Status != StatusPassed || !strings.Contains(result.Cache.Reason, "inputs changed during execution") {
		t.Fatalf("a result was cached under a key that predates the file its check created: %+v", result.Cache)
	}
}

// The racy window is measured from when the content was read, not from when
// the record was written: a hash taken in the same tick as a write stays
// untrusted however long the process ran before flushing it.
func TestPersistedEntryHashedWhileRacyIsDistrusted(t *testing.T) {
	cacheDir := t.TempDir()
	root := t.TempDir()
	path := filepath.Join(root, "input.go")
	old := time.Now().Add(-time.Hour)
	writeFile(t, path, sourceTwo)
	setModTime(t, path, old)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}
	want := mustSnapshot(t, sessionAt(""), req)
	stale := fmt.Sprintf("%o:%x", info.Mode().Perm(), sha256.Sum256([]byte(sourceOne)))

	for _, tt := range []struct {
		name     string
		hashedAt time.Time
		trusted  bool
	}{
		{name: "hashed in the modification tick", hashedAt: old.Add(time.Second)},
		{name: "hashed after the file settled", hashedAt: old.Add(time.Minute), trusted: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entry := statEntry{Stat: statOf(info), Value: stale, HashedAt: tt.hashedAt.UnixNano()}
			record := statRecord{Root: root, Entries: map[string]statEntry{"input.go": entry}}
			if err := writeRecord(filepath.Join(cacheDir, "stat", digest(root)+".json"), record, recordLimit); err != nil {
				t.Fatal(err)
			}

			// A new session reads the persisted record, the way a second CLI
			// process would.
			if got := mustSnapshot(t, sessionAt(cacheDir), req); (got != want) != tt.trusted {
				t.Fatalf("trusted = %v, want %v", got != want, tt.trusted)
			}
		})
	}
}

// A flush keeps what the next run can use and nothing else: entries this run
// saw, and unseen entries whose file still has the recorded stat. An entry for
// a deleted file is dropped.
func TestFlushPrunesEntriesForGoneFiles(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	root := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for _, name := range []string{"keep.go", "other.go", "gone.go"} {
		writeFile(t, filepath.Join(root, name), sourceOne)
		setModTime(t, filepath.Join(root, name), old)
	}
	session := sessionAt(cacheDir)
	mustSnapshot(t, session, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem})
	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}

	// A new session reads the persisted record, the way a second CLI process
	// would.
	session = sessionAt(cacheDir)
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	mustSnapshot(t, session, snapshotRequest{Root: root, Paths: []string{"keep.go"}, Discovery: DiscoveryFilesystem})
	if err := session.Flush(); err != nil {
		t.Fatal(err)
	}

	var record statRecord
	if err := readRecord(filepath.Join(cacheDir, "stat", digest(root)+".json"), &record, recordLimit); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"keep.go": true, "other.go": true, "gone.go": false} {
		if _, ok := record.Entries[name]; ok != want {
			t.Errorf("%s persisted = %v, want %v", name, ok, want)
		}
	}
}

// benchmarkTree is a synthetic repository large enough for the per-file cost to
// dominate: 5,000 small Go files spread over 50 directories.
func benchmarkTree(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	for i := range 5000 {
		path := filepath.Join(root, fmt.Sprintf("pkg%02d", i%50), fmt.Sprintf("file%04d.go", i))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, fmt.Appendf(nil, "package pkg%02d\n\nconst value%04d = %d\n", i%50, i, i), 0600); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

func BenchmarkFingerprint(b *testing.B) {
	root := benchmarkTree(b)
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	b.Run("cold", func(b *testing.B) {
		for b.Loop() {
			b.StopTimer()
			session := sessionAt("")
			b.StartTimer()
			if _, err := session.snapshot(b.Context(), req); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("warm", func(b *testing.B) {
		session := sessionAt("")
		if _, err := session.snapshot(b.Context(), req); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if _, err := session.snapshot(b.Context(), req); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Only a git-discovery target explains a fallback; a filesystem target asked
// for the walk and has nothing to explain.
func TestDiscoveryNoteExplainsAnIgnoredSourceForGitDiscoveryOnly(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, ".gitignore"), "*\n")
	writeFile(t, filepath.Join(root, "input.go"), sourceOne)

	if note := sessionAt("").discoveryNote(t.Context(), root, DiscoveryGit); !strings.Contains(note, "lists no files") {
		t.Fatalf("an ignored source must explain its filesystem fallback: %q", note)
	}
	if note := sessionAt("").discoveryNote(t.Context(), root, DiscoveryFilesystem); note != "" {
		t.Fatalf("a filesystem target has no fallback to explain: %q", note)
	}
}

// The stat record is only a hint, but a write that fails must still be
// reported rather than lost.
func TestStatFlushReportsAWriteFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A file where the stat directory belongs makes every record write fail.
	writeFile(t, filepath.Join(dir, "stat"), "not a directory")
	store := newStatStore(dir, recordLimit)
	store.prepare(t.TempDir())

	if err := store.flush(); err == nil {
		t.Fatal("a stat record that could not be written was not reported")
	}
}
