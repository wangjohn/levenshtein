package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fileSetRepository is a work tree holding every kind of path the file set has
// to classify: tracked, untracked, excluded, and private files, a symlink
// inside an excluded directory, and ignored paths the Go toolchain can and
// cannot load. Loadable: Go files in an ignored directory and one in an
// ordinary package directory, a directory a tracked package embeds (holding a
// symlink), testdata, and a symlink to a directory of Go files. Not loadable: a node_modules tree with a .bin
// symlink, a _build directory, a dependency directory, and loose ignored files,
// one named with pattern characters.
func fileSetRepository(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, ".gitignore"), "gen/\ndeps/\n*.log\n.env\nnode_modules/\n_build/\n*_generated.go\npkg/assets/\n*.out\n")
	for _, name := range []string{
		"main.go", "pkg/lib.go", "pkg/new.go", "gen/keep.pb.go", "gen/api.pb.go", "gen/deep/x.go", "pkg/zz_generated.go",
		"pkg/assets/data.txt", "pkg/testdata/golden.out", "node_modules/lib/index.js", "_build/app.bin",
		"deps/a/b.js", "debug.log", "!odd[1]*.log", ".env", "config/.env.local", "build/tracked.txt", "build/out.bin",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), sourceOne)
	}
	writeFile(t, filepath.Join(root, "pkg", "embed.go"), "package pkg\n\nimport _ \"embed\"\n\n//go:embed assets\nvar assets string\n")
	for link, target := range map[string]string{"build/link": "../main.go", "node_modules/.bin/tool": "../lib/index.js", "pkg/assets/link": "data.txt", "gen/current": "deep"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, link)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", ".gitignore", "main.go", "pkg/lib.go", "pkg/embed.go", "build/tracked.txt")
	runGit(t, root, "add", "-f", "gen/keep.pb.go")
	relist(root)
	return root
}

// planFor plans one check of kind over the whole source less build/, in env.
func planFor(t *testing.T, root string, kind CheckKind, executor ExecutorKind) PlannedCheck {
	t.Helper()
	config := fmt.Sprintf(`{"version":1,"targets":{"all":{"dir":".","inputs":["."],"exclude":["build"]}},"environments":{"env":{"executor":%q}},"checks":{"c":{"kind":%q,"target":"all","environment":"env"}},"runs":{"r":{"checks":["c"]}}}`, executor, kind)
	cfg, err := Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cfg.Plan(root, "r")
	if err != nil {
		t.Fatal(err)
	}
	return plan.Checks[0]
}

// The Go toolchain reads every file in a package directory, including
// generated code the work tree ignores, so a Go kind's key must change when an
// ignored Go file does, or go vet replays a pass over code it never saw.
func TestGoKindKeyCoversIgnoredGoFiles(t *testing.T) {
	root := fileSetRepository(t)
	stats.configure(t.TempDir())
	for _, executor := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		t.Run(string(executor), func(t *testing.T) {
			req := Request{Source: root, Shared: t.TempDir(), PlannedCheck: planFor(t, root, CheckGoVet, executor)}
			before, err := fingerprint(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}

			writeFile(t, filepath.Join(root, "gen", "api.pb.go"), sourceTwo+string(executor))
			after, err := fingerprint(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if after == before {
				t.Fatal("editing an ignored Go file left the go-vet key unchanged")
			}
		})
	}
}

// An ignored tree the Go toolchain cannot load, such as node_modules with its
// .bin symlinks, must neither disable result reuse nor change the key, while
// an ignored directory a package embeds changes it, links included.
func TestGoKindKeySkipsUnloadableIgnoredTrees(t *testing.T) {
	root := fileSetRepository(t)
	stats.configure(t.TempDir())
	for _, executor := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		t.Run(string(executor), func(t *testing.T) {
			req := Request{Source: root, Shared: t.TempDir(), PlannedCheck: planFor(t, root, CheckGoVet, executor)}
			key := func() string {
				t.Helper()
				got, err := fingerprint(t.Context(), req)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}

			before := key()
			for _, name := range []string{"node_modules/lib/index.js", "_build/app.bin", "debug.log"} {
				writeFile(t, filepath.Join(root, filepath.FromSlash(name)), sourceTwo+string(executor))
			}
			if key() != before {
				t.Fatal("editing ignored files the Go toolchain cannot load changed the key")
			}

			writeFile(t, filepath.Join(root, "pkg", "assets", "data.txt"), sourceThree+string(executor))
			embedded := key()
			if embedded == before {
				t.Fatal("editing an embedded ignored file left the key unchanged")
			}
			link := filepath.Join(root, "pkg", "assets", "link")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("other-"+string(executor), link); err != nil {
				t.Fatal(err)
			}
			if key() == embedded {
				t.Fatal("retargeting an embedded ignored link left the key unchanged")
			}
		})
	}
}

// An import path resolves through a symlinked directory, so retargeting an
// ignored link from one tracked package to another changes what the Go
// toolchain compiles, and must change a Go kind's key.
func TestGoKindKeyCoversIgnoredDirectoryLinks(t *testing.T) {
	root := fileSetRepository(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "gen/\ndeps/\n*.log\n.env\nnode_modules/\n_build/\n*_generated.go\npkg/assets/\n*.out\n/impl\n")
	writeFile(t, filepath.Join(root, "impl_a", "a.go"), sourceOne)
	writeFile(t, filepath.Join(root, "impl_b", "b.go"), sourceTwo)
	if err := os.Symlink("impl_a", filepath.Join(root, "impl")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".gitignore", "impl_a", "impl_b")
	relist(root)
	stats.configure(t.TempDir())
	req := Request{Source: root, Shared: t.TempDir(), PlannedCheck: planFor(t, root, CheckGoVet, ExecutorNative)}

	before, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "impl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("impl_b", filepath.Join(root, "impl")); err != nil {
		t.Fatal(err)
	}

	after, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("retargeting an ignored directory link left the go-vet key unchanged")
	}
}

// An ignored link out of the source, such as a Nix result link, names nothing
// the key or the go-generate copy could hold, so it stays out of both rather
// than failing the copy.
func TestGoKindOmitsIgnoredLinksOutOfTheSource(t *testing.T) {
	root := fileSetRepository(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "gen/\ndeps/\n*.log\n.env\nnode_modules/\n_build/\n*_generated.go\npkg/assets/\n*.out\n/result\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "result")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".gitignore")
	relist(root)
	req := Request{Source: root, PlannedCheck: planFor(t, root, CheckGoGenerate, ExecutorNative)}

	err := copyInputs(t.Context(), req, t.TempDir())
	if err != nil {
		t.Fatalf("an ignored link out of the source failed the go-generate copy: %v", err)
	}
}

// With a vendor directory the go command builds from vendor/modules.txt,
// refusing a tree whose manifest disagrees with go.mod, so an edit to an
// ignored manifest must change a Go kind's key.
func TestGoKindKeyCoversIgnoredVendorManifest(t *testing.T) {
	root := fileSetRepository(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "gen/\ndeps/\n*.log\n.env\nnode_modules/\n_build/\n*_generated.go\npkg/assets/\n*.out\nvendor/\n")
	writeFile(t, filepath.Join(root, "vendor", "example.com", "dep", "dep.go"), sourceOne)
	writeFile(t, filepath.Join(root, "vendor", "modules.txt"), "# example.com/dep v1.0.0\n## explicit; go 1.21\nexample.com/dep\n")
	runGit(t, root, "add", ".gitignore")
	relist(root)
	stats.configure(t.TempDir())
	req := Request{Source: root, Shared: t.TempDir(), PlannedCheck: planFor(t, root, CheckGoVet, ExecutorNative)}

	before, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "vendor", "modules.txt"), "# example.com/dep v1.0.0\n## explicit; go 1.22\nexample.com/dep\n")

	after, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("editing an ignored vendor/modules.txt left the go-vet key unchanged")
	}
}

// A native scanner reads exactly what the key hashed: under git discovery an
// ignored file is in neither, so adding a secret to one cannot replay a pass
// over a scan that read it.
func TestVisibleFilesLeaveOutIgnoredFiles(t *testing.T) {
	root := fileSetRepository(t)
	req := Request{Source: root, PlannedCheck: planFor(t, root, CheckSecrets, ExecutorNative)}

	files, err := visibleFiles(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "gen/keep.pb.go", "main.go", "pkg/embed.go", "pkg/lib.go", "pkg/new.go"}
	if !slices.Equal(files, want) {
		t.Fatalf("visible files %v, want %v", files, want)
	}
}

// An input spelled differently from the work tree resolves on a
// case-insensitive filesystem but matches nothing git lists; it must not
// silently contribute nothing to the key.
func TestMisspelledInputIsRejected(t *testing.T) {
	root := fileSetRepository(t)
	probe, err := os.Lstat(filepath.Join(root, "PKG"))
	if err != nil || !probe.IsDir() {
		t.Skip("the filesystem is case-sensitive, so a misspelled input is simply missing")
	}

	_, err = snapshot(t.Context(), snapshotRequest{Root: root, Paths: []string{"PKG"}, Discovery: DiscoveryGit})
	if err == nil || !strings.Contains(err.Error(), `"pkg"`) {
		t.Fatalf("a misspelled input was fingerprinted as empty: %v", err)
	}

	config := `{"version":1,"targets":{"t":{"dir":".","inputs":["PKG"]}},"environments":{"e":{"executor":"native"}},"checks":{"c":{"kind":"secrets","target":"t","environment":"e"}},"runs":{"r":{"checks":["c"]}}}`
	cfg, err := Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Plan(root, "r"); err == nil || !strings.Contains(err.Error(), `"pkg"`) {
		t.Fatalf("planned a misspelled input: %v", err)
	}
}

// An input reached through a symlinked directory is an alias. Under either
// discovery it follows the source symlink policy, which disables result reuse,
// rather than hashing nothing (git) or the link target's files (filesystem).
func TestInputThroughASymlinkFollowsTheSymlinkPolicy(t *testing.T) {
	root := fileSetRepository(t)
	if err := os.Symlink("pkg", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "link")
	relist(root)

	for _, discovery := range discoveryKinds {
		t.Run(string(discovery), func(t *testing.T) {
			_, err := snapshot(t.Context(), snapshotRequest{Root: root, Paths: []string{filepath.Join("link", "lib.go")}, Discovery: discovery})
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("an input through a symlink was fingerprinted: %v", err)
			}
		})
	}
}

// git lists an untracked nested repository as one path, so an input inside it
// has nothing listed under it; its files must still enter the key.
func TestInputInsideANestedRepositoryIsFingerprinted(t *testing.T) {
	root := fileSetRepository(t)
	writeFile(t, filepath.Join(root, "nested", "lib.go"), sourceOne)
	runGit(t, filepath.Join(root, "nested"), "init")
	relist(root)
	stats.configure(t.TempDir())
	req := snapshotRequest{Root: root, Paths: []string{filepath.Join("nested", "lib.go")}, Discovery: DiscoveryGit}

	before := mustSnapshot(t, req)
	writeFile(t, filepath.Join(root, "nested", "lib.go"), sourceThree+sourceThree)
	if mustSnapshot(t, req) == before {
		t.Fatal("editing an input inside a nested repository left the key unchanged")
	}
}

// The files each consumer of fileSetRepository's target (all of it, less
// build/) reads. A git-discovery kind reads what git lists. A Go kind also
// reads the ignored paths the Go toolchain can load: Go files, what a package
// embeds, and testdata, but nothing under node_modules, _build, or deps, which
// hold no Go file, nor loose ignored files no package embeds. Private files are
// hashed but never shown to a scanner or a container.
var (
	listedFiles  = []string{".gitignore", "config/.env.local", "gen/keep.pb.go", "main.go", "pkg/embed.go", "pkg/lib.go", "pkg/new.go"}
	goFiles      = []string{".gitignore", "config/.env.local", "gen/api.pb.go", "gen/current", "gen/deep/x.go", "gen/keep.pb.go", "main.go", "pkg/assets/data.txt", "pkg/assets/link", "pkg/embed.go", "pkg/lib.go", "pkg/new.go", "pkg/testdata/golden.out", "pkg/zz_generated.go"}
	privateFiles = []string{".env", "config/.env.local"}
)

// keyedFiles are the files and links a check's key hashes, as sorted
// forward-slash paths. Entries for directories and missing inputs are not
// files anything reads.
func keyedFiles(t *testing.T, req Request) []string {
	t.Helper()
	entries, err := snapshotEntries(t.Context(), keyedSource(req))
	if err != nil {
		t.Fatal(err)
	}

	var files []string
	for name := range entries {
		if info, err := os.Lstat(filepath.Join(req.Source, name)); err == nil && !info.IsDir() {
			files = append(files, filepath.ToSlash(name))
		}
	}
	slices.Sort(files)
	return files
}

func withoutPrivate(files []string) []string {
	return slices.DeleteFunc(slices.Clone(files), func(file string) bool { return slices.Contains(privateFiles, file) })
}

// excludedPaths are the paths the Dagger import leaves out beyond the fixed
// private patterns, one pattern per path.
func excludedPaths(t *testing.T, req Request) []string {
	t.Helper()
	excludes, err := importExcludes(t.Context(), targetFiles(req))
	if err != nil {
		t.Fatal(err)
	}

	var named []string
	for _, pattern := range excludes {
		if !strings.HasSuffix(pattern, "/**") {
			named = append(named, pattern)
		}
	}
	slices.Sort(named)
	return named
}

// TestFileSetConformance is the contract H-1 and M-1 broke: for every kind of
// path, what a check's key hashes is what its executor reads. Each executor
// sees the key's set less the private files; the Dagger import itself is
// compared in TestDaggerImportMatchesTheKey, which needs an engine.
func TestFileSetConformance(t *testing.T) {
	root := fileSetRepository(t)
	stats.configure(t.TempDir())

	t.Run("git discovery", func(t *testing.T) {
		req := Request{Source: root, PlannedCheck: planFor(t, root, CheckSecrets, ExecutorNative)}
		if got := keyedFiles(t, req); !slices.Equal(got, listedFiles) {
			t.Fatalf("key hashes %v, want %v", got, listedFiles)
		}

		visible, err := visibleFiles(t.Context(), req, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := withoutPrivate(listedFiles); !slices.Equal(visible, want) {
			t.Fatalf("native scanners read %v, want %v", visible, want)
		}

		// Everything the listing leaves out is excluded from the Dagger import,
		// literally, and a wholly ignored directory as one path. gen/ holds a
		// tracked file, so its ignored paths are excluded one by one.
		want := []string{`\!odd\[1]\*.log`, "_build", "debug.log", "deps", "gen/api.pb.go", "gen/current", "gen/deep", "node_modules", "pkg/assets", "pkg/testdata", "pkg/zz_generated.go"}
		if got := excludedPaths(t, req); !slices.Equal(got, want) {
			t.Errorf("the Dagger import excludes %v, want %v", got, want)
		}
	})

	// The Go toolchain reads what it can load, so the key covers it and the
	// Dagger import leaves out exactly the rest, on either executor; the native
	// go-generate copy holds the same files.
	t.Run("Go kind", func(t *testing.T) {
		for _, executor := range []ExecutorKind{ExecutorNative, ExecutorDagger} {
			req := Request{Source: root, PlannedCheck: planFor(t, root, CheckGoVet, executor)}
			if got := keyedFiles(t, req); !slices.Equal(got, goFiles) {
				t.Fatalf("%s: key hashes %v, want %v", executor, got, goFiles)
			}
			want := []string{`\!odd\[1]\*.log`, "_build", "debug.log", "deps", "node_modules"}
			if got := excludedPaths(t, req); !slices.Equal(got, want) {
				t.Fatalf("%s: the Dagger import excludes %v, want %v", executor, got, want)
			}
		}

		dest := t.TempDir()
		req := Request{Source: root, PlannedCheck: planFor(t, root, CheckGoGenerate, ExecutorNative)}
		if err := copyInputs(t.Context(), req, dest); err != nil {
			t.Fatal(err)
		}
		var copied []string
		for _, file := range listTree(t, dest) {
			copied = append(copied, filepath.ToSlash(file))
		}
		if want := withoutPrivate(goFiles); !slices.Equal(copied, want) {
			t.Fatalf("go-generate copied %v, want %v", copied, want)
		}
	})

	// A symlink inside declared content is refused by the key and by the
	// Dagger import alike; neither follows it.
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(root, "pkg", "link.go")
		if err := os.Symlink("lib.go", link); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Remove(link) }()
		relist(root)
		defer relist(root)

		for _, kind := range []CheckKind{CheckSecrets, CheckGoVet} {
			req := Request{Source: root, PlannedCheck: planFor(t, root, kind, ExecutorDagger)}
			if _, err := snapshot(t.Context(), keyedSource(req)); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Errorf("%s: the key hashed a symlink: %v", kind, err)
			}
			if _, err := importExcludes(t.Context(), targetFiles(req)); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Errorf("%s: the Dagger import accepted a symlink: %v", kind, err)
			}
		}
	})
}

// A listing is sorted, so an input's paths are one contiguous run. The run must
// hold the input itself and everything below it, and nothing that merely
// shares its first bytes.
func TestListedUnderFindsExactlyAnInputsPaths(t *testing.T) {
	sep := string(filepath.Separator)
	listed := []string{"a", "a-b", "a.go", "a" + sep + "x", "a" + sep + "y" + sep + "z", "ab", "b" + sep + "a"}
	slices.Sort(listed)

	for input, want := range map[string][]string{
		"a":             {"a", "a" + sep + "x", "a" + sep + "y" + sep + "z"},
		"a" + sep + "y": {"a" + sep + "y" + sep + "z"},
		"a.go":          {"a.go"},
		"b":             {"b" + sep + "a"},
		"c":             nil,
		".":             listed,
	} {
		if got := listedUnder(listed, input); !slices.Equal(got, want) {
			t.Errorf("listedUnder(%q) = %v, want %v", input, got, want)
		}
	}
}
