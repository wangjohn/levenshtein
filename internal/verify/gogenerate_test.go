package verify

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/testgit"
)

// listTree names every file and link under root, relative to it.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		files = append(files, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

// The scratch copy holds what the Dagger path would import, less what the
// repository ignores under git discovery: declared inputs only, without
// excludes, .git, or private .env files, and a relative link stays a link.
func TestCopyInputsCopiesWhatTheCheckMayRead(t *testing.T) {
	for _, discovery := range []DiscoveryKind{DiscoveryFilesystem, DiscoveryGit} {
		t.Run(string(discovery), func(t *testing.T) {
			req := nativeRequest(t)
			req.Target.Inputs = []string{"app", "go.work", "missing"}
			req.Target.Exclude = []string{filepath.Join("app", "node_modules")}
			req.Target.Discovery = discovery
			for name, content := range map[string]string{
				"go.work":                      "go 1.24\n",
				"other/secret.go":              "package other\n",
				"app/main.go":                  "package main\n",
				"app/.env":                     "TOKEN=x\n",
				"app/.env.example":             "TOKEN=\n",
				"app/.gitignore":               "build/\n",
				"app/build/out.bin":            "ignored output\n",
				"app/node_modules/dep/x.js":    "excluded\n",
				"app/internal/gen/template.go": "package gen\n",
			} {
				writeTestFile(t, filepath.Join(req.Source, name), content)
			}
			if err := os.Symlink("main.go", filepath.Join(req.Source, "app", "link.go")); err != nil {
				t.Fatal(err)
			}
			if discovery == DiscoveryGit {
				testgit.Run(t, testgit.Path(t), req.Source, "init", "--quiet")
				relist(req.Source)
			}

			dest := t.TempDir()
			if err := copyInputs(context.Background(), req, dest); err != nil {
				t.Fatal(err)
			}
			want := []string{"app/.env.example", "app/.gitignore", "app/build/out.bin", "app/internal/gen/template.go", "app/link.go", "app/main.go", "go.work"}
			if discovery == DiscoveryGit {
				want = slices.DeleteFunc(want, func(name string) bool { return name == "app/build/out.bin" })
			}
			for i := range want {
				want[i] = filepath.FromSlash(want[i])
			}
			if got := listTree(t, dest); !slices.Equal(got, want) {
				t.Fatalf("copied %v, want %v", got, want)
			}
			if link, err := os.Readlink(filepath.Join(dest, "app", "link.go")); err != nil || link != "main.go" {
				t.Fatalf("a relative link must stay a link: %q %v", link, err)
			}
		})
	}
}

// The copy is where generators run, so each file must arrive with its bytes
// and its mode, and a file that cannot be copied must fail the copy.
func TestCopyFileKeepsContentAndReportsFailures(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "gen.sh"), "#!/bin/sh\necho generated\n")
	if err := os.Chmod(filepath.Join(source, "gen.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup.
	dest := t.TempDir()

	if err := copyFile(root, "gen.sh", filepath.Join(dest, "gen.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "gen.sh"))
	if err != nil || string(data) != "#!/bin/sh\necho generated\n" {
		t.Fatalf("the copy must keep the file's bytes: %q %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(dest, "gen.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the copy must keep an executable generator executable: %v %v", info, err)
	}

	if err := copyFile(root, "gen.sh", filepath.Join(dest, "gen.sh"), 0o755); err == nil {
		t.Error("copying over a file already in the copy must fail")
	}
	if err := copyFile(root, "dir", filepath.Join(dest, "dir"), 0o755); err == nil {
		t.Error("a file whose content cannot be read must fail the copy")
	}
}

// A generator writes through the links in its copy, so a link that leads out
// of the copy could reach the working tree.
func TestCopyInputsRefusesLinksOutOfTheCopy(t *testing.T) {
	for name, link := range map[string]string{
		"absolute": filepath.Join(t.TempDir(), "outside.go"),
		"relative": filepath.Join("..", "..", "outside.go"),
	} {
		req := nativeRequest(t)
		req.Target.Inputs = []string{"app"}
		writeTestFile(t, filepath.Join(req.Source, "app", "main.go"), "package main\n")
		if err := os.Symlink(link, filepath.Join(req.Source, "app", "link.go")); err != nil {
			t.Fatal(err)
		}

		err := copyInputs(context.Background(), req, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "outside the target's inputs") {
			t.Errorf("%s: a link out of the copy must be refused: %v", name, err)
		}
	}
}

// go-generate's generated files are judged against the declared inputs alone,
// so its verdict is reused like go-vet's until a fresh run.
func TestGenerateResultsAreReusedUntilAFreshRun(t *testing.T) {
	if alwaysFresh[CheckGoGenerate] != "" || !sharedGoChecks[CheckGoGenerate] {
		t.Fatal("go-generate must be cacheable on either executor")
	}
	req := cacheRequest(t)
	req.Check.Kind = CheckGoGenerate
	if executionNonce(req) != "" {
		t.Fatal("an ordinary go-generate run should keep Dagger's cache")
	}
	req.RerunChecks = true
	if executionNonce(req) == "" {
		t.Fatal("a fresh go-generate run must get unique Dagger execution inputs")
	}
}
