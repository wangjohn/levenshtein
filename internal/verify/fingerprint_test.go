package verify

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

func mustSnapshot(t *testing.T, req snapshotRequest) string {
	t.Helper()
	value, err := snapshot(req)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// restart drops the in-memory memo so the next snapshot has to read the
// persisted record, the way a second CLI process would.
func restart(t *testing.T, dir string) {
	t.Helper()
	stats.configure(t.TempDir())
	stats.configure(dir)
}

// The memo trades a content read for a stat comparison, so an edit that
// preserves every stat field is invisible within a process, and a change to any
// one of size, modification time or inode is not.
func TestFileStatMemoReusesOnlyIdenticalStats(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input.go")
	writeFile(t, path, sourceOne)
	stamp := modTime(t, path)
	stats.configure(t.TempDir())
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, req)

	writeFile(t, path, sourceTwo)
	setModTime(t, path, stamp)
	if reused := mustSnapshot(t, req); reused != before {
		t.Fatal("identical stat did not reuse the memoized hash")
	}

	// Modification time: the memo now holds the first content for a stat that no
	// longer matches, so the second content has to be read.
	setModTime(t, path, stamp.Add(time.Second))
	edited := mustSnapshot(t, req)
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
	replaced := mustSnapshot(t, req)
	if replaced == edited {
		t.Fatal("replaced file reused the memoized hash")
	}

	// Size.
	writeFile(t, path, sourceThree+"\n")
	setModTime(t, path, stamp.Add(time.Second))
	if grown := mustSnapshot(t, req); grown == replaced {
		t.Fatal("changed size reused the memoized hash")
	}
}

// A persisted entry is a hint the next process revalidates. It is only usable
// when the record was written well after the file settled; git's racy-index
// window is what decides that.
func TestPersistedStatCacheHonorsTheRacyWindow(t *testing.T) {
	cacheDir := t.TempDir()
	settled := t.TempDir()
	settledPath := filepath.Join(settled, "input.go")
	old := time.Now().Add(-time.Hour)
	writeFile(t, settledPath, sourceOne)
	setModTime(t, settledPath, old)
	stats.configure(cacheDir)
	req := snapshotRequest{Root: settled, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, req)
	if err := (&Cache{Dir: cacheDir}).Flush(); err != nil {
		t.Fatal(err)
	}

	restart(t, cacheDir)
	writeFile(t, settledPath, sourceTwo)
	setModTime(t, settledPath, old)
	if reused := mustSnapshot(t, req); reused != before {
		t.Fatal("a settled persisted entry was not reused")
	}

	// A plain touch is not an edit: the stat no longer matches, so the file is
	// read again and yields the same content hash.
	touched := t.TempDir()
	touchedPath := filepath.Join(touched, "input.go")
	writeFile(t, touchedPath, sourceOne)
	setModTime(t, touchedPath, old)
	touchedReq := snapshotRequest{Root: touched, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	touchedBefore := mustSnapshot(t, touchedReq)
	if err := (&Cache{Dir: cacheDir}).Flush(); err != nil {
		t.Fatal(err)
	}

	restart(t, cacheDir)
	setModTime(t, touchedPath, time.Now())
	if after := mustSnapshot(t, touchedReq); after != touchedBefore {
		t.Fatal("touching a file changed the digest")
	}

	// The same size-preserving edit against a record written while the file was
	// still racy must be detected instead.
	racy := t.TempDir()
	racyPath := filepath.Join(racy, "input.go")
	writeFile(t, racyPath, sourceOne)
	stamp := modTime(t, racyPath)
	racyReq := snapshotRequest{Root: racy, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	racyBefore := mustSnapshot(t, racyReq)
	if err := (&Cache{Dir: cacheDir}).Flush(); err != nil {
		t.Fatal(err)
	}

	restart(t, cacheDir)
	writeFile(t, racyPath, sourceTwo)
	setModTime(t, racyPath, stamp)
	if hidden := mustSnapshot(t, racyReq); hidden == racyBefore {
		t.Fatal("a racy persisted entry hid a same-size edit")
	}
}

func TestUnreadableStatRecordIsNotFatal(t *testing.T) {
	cacheDir := t.TempDir()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "input.go"), sourceOne)
	stats.configure(cacheDir)
	req := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	want := mustSnapshot(t, req)
	if err := (&Cache{Dir: cacheDir}).Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "stat", digest(root)+".json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}

	restart(t, cacheDir)
	if got := mustSnapshot(t, req); got != want {
		t.Fatalf("corrupt record changed the digest: %q want %q", got, want)
	}
}

// runGit runs git in root with a throwaway HOME, so the developer's own
// configuration cannot shape a fixture.
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
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
// submodule is listed as a single gitlink, so its contents are walked.
func TestGitDiscoveryFollowsTheWorkTree(t *testing.T) {
	root := gitRepository(t)
	runGit(t, root, "update-index", "--add", "--cacheinfo", "160000,0123456789abcdef0123456789abcdef01234567,sub")
	writeFile(t, filepath.Join(root, "sub", "lib.go"), sourceOne)
	stats.configure(t.TempDir())
	git := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryGit}
	host := snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, git)
	if before == mustSnapshot(t, host) {
		t.Fatal("git and filesystem discovery cannot agree while an ignored file exists")
	}

	writeFile(t, filepath.Join(root, "generated", "client.go"), sourceTwo+sourceTwo)
	if ignored := mustSnapshot(t, git); ignored != before {
		t.Fatal("an ignored file entered the fingerprint")
	}
	if watched := mustSnapshot(t, host); watched == before {
		t.Fatal("filesystem discovery lost the ignored file")
	}

	for _, name := range []string{"tracked.go", "untracked.go", "sub/lib.go"} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), sourceTwo+sourceTwo)
		changed := mustSnapshot(t, git)
		if changed == before {
			t.Fatalf("editing %s did not change the fingerprint", name)
		}
		before = changed
	}

	// A tracked file that is not on disk is absent rather than "missing", and no
	// directory has an entry of its own except the walked submodule, so the same
	// content in a fresh clone fingerprints the same way.
	dir, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	listed, ok := gitFiles(root)
	if !ok {
		t.Fatal("git listing unavailable inside a work tree")
	}

	entries := map[string]string{}
	var files []hashTarget
	if err := walkListing(dir, listed, ".", git, entries, &files); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries["sub"] != "directory" {
		t.Fatalf("git discovery recorded directories or missing paths: %v", entries)
	}
	hashed := map[string]bool{}
	for _, file := range files {
		hashed[filepath.ToSlash(file.Path)] = true
	}
	for _, name := range []string{".gitignore", "tracked.go", "untracked.go", "sub/lib.go"} {
		if !hashed[name] {
			t.Fatalf("%s was not enumerated: %v", name, hashed)
		}
	}
	for _, name := range []string{"removed.go", "generated/client.go"} {
		if hashed[name] {
			t.Fatalf("%s should not be enumerated: %v", name, hashed)
		}
	}
}

// A declared input that exists nowhere is still recorded as missing, so adding
// it later invalidates the cache exactly as the filesystem walk does. A declared
// input the work tree ignores exists, and so contributes nothing at all.
func TestGitDiscoveryRecordsMissingButNotIgnoredInputs(t *testing.T) {
	root := gitRepository(t)
	stats.configure(t.TempDir())
	req := snapshotRequest{Root: root, Discovery: DiscoveryGit}
	dir, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	listed, ok := gitFiles(root)
	if !ok {
		t.Fatal("git listing unavailable inside a work tree")
	}

	for _, tt := range []struct {
		path string
		want string
	}{{path: "optional.go", want: "missing"}, {path: "generated", want: ""}, {path: "removed.go", want: ""}} {
		t.Run(tt.path, func(t *testing.T) {
			entries := map[string]string{}
			var files []hashTarget
			if err := walkListing(dir, listed, tt.path, req, entries, &files); err != nil {
				t.Fatal(err)
			}
			if entries[tt.path] != tt.want || len(files) != 0 {
				t.Fatalf("entries = %v, files = %v", entries, files)
			}
		})
	}
}

// Outside a work tree git discovery is not an error, it is just unavailable.
func TestGitDiscoveryFallsBackOutsideAWorkTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "input.go"), sourceOne)
	stats.configure(t.TempDir())

	git := mustSnapshot(t, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryGit})
	host := mustSnapshot(t, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem})
	if git != host {
		t.Fatal("a directory that is not a work tree must fall back to the filesystem walk")
	}
	if note := discoveryNote(root, DiscoveryGit); note != "" {
		t.Fatalf("ordinary fallback reported a problem: %q", note)
	}
}

func TestTargetExcludeLeavesPathsOutOfTheFingerprint(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"), sourceOne)
	writeFile(t, filepath.Join(root, "build", "out.bin"), sourceOne)
	stats.configure(t.TempDir())
	req := snapshotRequest{Root: root, Paths: []string{"."}, Excludes: []string{"build"}, Discovery: DiscoveryFilesystem}

	before := mustSnapshot(t, req)

	writeFile(t, filepath.Join(root, "build", "out.bin"), sourceTwo+sourceTwo)
	if excluded := mustSnapshot(t, req); excluded != before {
		t.Fatal("an excluded path entered the fingerprint")
	}
	writeFile(t, filepath.Join(root, "src", "main.go"), sourceTwo+sourceTwo)
	if included := mustSnapshot(t, req); included == before {
		t.Fatal("exclude swallowed a declared input")
	}

	want := []string{"**/.env", "**/.env.*", "!**/.env.example", "**/.git", "build", "build/**"}
	got := daggerExcludes([]string{"build"})
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Dagger excludes = %v, want %v", got, want)
	}
}

func TestPlanValidatesDiscoveryAndExclude(t *testing.T) {
	const config = `{"version":1,"targets":{"app":{"dir":".","inputs":["."]%s}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"}},"runs":{"branch":{"checks":["lint"]}}}`
	for _, tt := range []struct {
		name   string
		target string
		want   DiscoveryKind
	}{
		{name: "default", want: DiscoveryGit},
		{name: "explicit filesystem", target: `,"discovery":"filesystem"`, want: DiscoveryFilesystem},
		{name: "explicit git", target: `,"discovery":"git","exclude":["node_modules","build"]`, want: DiscoveryGit},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(fmt.Sprintf(config, tt.target)))
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
			cfg, err := Parse([]byte(fmt.Sprintf(config, target)))
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

	listed, ok := gitFiles(root)
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
	if _, ok := gitFiles(root); !ok {
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
	requireGit(t)
	req := cacheRequest(t)
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
	stats.configure(t.TempDir())
	want := mustSnapshot(t, req)
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
			if err := writeRecord(filepath.Join(cacheDir, "stat", digest(root)+".json"), record); err != nil {
				t.Fatal(err)
			}

			restart(t, cacheDir)
			if got := mustSnapshot(t, req); (got != want) != tt.trusted {
				t.Fatalf("trusted = %v, want %v", got != want, tt.trusted)
			}
		})
	}
}

// A flush keeps what the next run can use and nothing else: entries this run
// saw, and unseen entries whose file still has the recorded stat. An entry for
// a deleted file is dropped.
func TestFlushPrunesEntriesForGoneFiles(t *testing.T) {
	cacheDir := t.TempDir()
	root := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for _, name := range []string{"keep.go", "other.go", "gone.go"} {
		writeFile(t, filepath.Join(root, name), sourceOne)
		setModTime(t, filepath.Join(root, name), old)
	}
	stats.configure(cacheDir)
	mustSnapshot(t, snapshotRequest{Root: root, Paths: []string{"."}, Discovery: DiscoveryFilesystem})
	if err := (&Cache{Dir: cacheDir}).Flush(); err != nil {
		t.Fatal(err)
	}

	restart(t, cacheDir)
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	mustSnapshot(t, snapshotRequest{Root: root, Paths: []string{"keep.go"}, Discovery: DiscoveryFilesystem})
	if err := (&Cache{Dir: cacheDir}).Flush(); err != nil {
		t.Fatal(err)
	}

	var record statRecord
	if err := readRecord(filepath.Join(cacheDir, "stat", digest(root)+".json"), &record); err != nil {
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
			stats.configure(b.TempDir())
			b.StartTimer()
			if _, err := snapshot(req); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("warm", func(b *testing.B) {
		stats.configure(b.TempDir())
		if _, err := snapshot(req); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if _, err := snapshot(req); err != nil {
				b.Fatal(err)
			}
		}
	})
}
