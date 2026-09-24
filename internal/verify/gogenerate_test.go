package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
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
				git, err := exec.LookPath("git")
				if err != nil {
					t.Skip("git is not installed")
				}
				cmd := exec.CommandContext(t.Context(), git, "init", "--quiet")
				cmd.Dir = req.Source
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git init: %v\n%s", err, output)
				}
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
