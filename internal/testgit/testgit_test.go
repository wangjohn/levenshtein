package testgit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// decoy commits one file in a repository standing in for the caller's own,
// and points the inherited git variables at it the way a hook would.
func decoy(t *testing.T, git string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	Run(t, git, dir, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "decoy.txt"), []byte("decoy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	Run(t, git, dir, "add", ".")
	Run(t, git, dir, "commit", "--quiet", "-m", "decoy")

	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
	t.Setenv("GIT_WORK_TREE", dir)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(dir, ".git", "index"))
	return dir
}

// state is what a stray command would change in the decoy: its commits, its
// branch, and its index.
func state(t *testing.T, git, dir string) string {
	t.Helper()
	index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), git, "-C", dir, "--git-dir", filepath.Join(dir, ".git"), "log", "--all", "--format=%H %D")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	return string(out) + string(index)
}

func TestCommandsDoNotReachAnInheritedRepository(t *testing.T) {
	git := Path(t)
	repo := decoy(t, git)
	before := state(t, git, repo)
	fixture := t.TempDir()

	// The inherited variables do redirect a command that keeps them.
	leaky := exec.CommandContext(t.Context(), git, "rev-parse", "--absolute-git-dir")
	leaky.Dir = fixture
	if out, err := leaky.Output(); err != nil || strings.TrimSpace(string(out)) != filepath.Join(repo, ".git") {
		t.Fatalf("the decoy must capture an unisolated command: %q %v", out, err)
	}

	Run(t, git, fixture, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(fixture, "fixture.txt"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	Run(t, git, fixture, "add", ".")
	Run(t, git, fixture, "commit", "--quiet", "-m", "fixture")
	Run(t, git, fixture, "switch", "--quiet", "-c", "feature")

	if after := state(t, git, repo); after != before {
		t.Fatalf("the decoy changed:\nbefore %s\nafter  %s", before, after)
	}
	if log := Run(t, git, fixture, "log", "--format=%s"); strings.TrimSpace(log) != "fixture" {
		t.Fatalf("the fixture must hold its own commit: %q", log)
	}
}

func TestIsolateClearsInheritedVariables(t *testing.T) {
	git := Path(t)
	repo := decoy(t, git)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'user.name'='leak'")

	Isolate(t)

	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_DIR=") || strings.HasPrefix(entry, "GIT_WORK_TREE=") || strings.HasPrefix(entry, "GIT_INDEX_FILE=") || strings.HasPrefix(entry, "GIT_CONFIG_PARAMETERS=") {
			t.Fatalf("%s survived isolation", entry)
		}
	}
	if os.Getenv("GIT_CONFIG_GLOBAL") != os.DevNull || os.Getenv("GIT_AUTHOR_NAME") != "t" {
		t.Fatalf("isolation variables missing: %v", os.Environ())
	}
	// Code under test runs git with the process environment.
	cmd := exec.CommandContext(t.Context(), git, "rev-parse", "--absolute-git-dir")
	cmd.Dir = t.TempDir()
	if out, err := cmd.Output(); err == nil && strings.Contains(string(out), repo) {
		t.Fatalf("the process environment still reaches the decoy: %s", out)
	}
}

// TestNoTestRunsGitDirectly keeps every git command in the module's tests on
// this package, so a new fixture cannot inherit the caller's repository. It
// flags exec.Command and exec.CommandContext whose program is the literal
// "git" or a variable or field named git.
func TestNoTestRunsGitDirectly(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	var violations []string
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDir(root, here, path, entry.Name())
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && runsGit(call) {
				violations = append(violations, fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("run git in tests through internal/testgit, which drops inherited GIT_ variables:\n%s", strings.Join(violations, "\n"))
	}
}

// skipDir leaves out this package, fixtures, hidden directories, and nested
// modules, which cannot import this package.
func skipDir(root, here, path, name string) error {
	if path == root {
		return nil
	}
	if path == here || name == "testdata" || strings.HasPrefix(name, ".") {
		return filepath.SkipDir
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return filepath.SkipDir
	}
	return nil
}

func runsGit(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return false
	}
	program := 0
	switch selector.Sel.Name {
	case "Command":
	case "CommandContext":
		program = 1
	default:
		return false
	}
	if len(call.Args) <= program {
		return false
	}
	switch arg := call.Args[program].(type) {
	case *ast.BasicLit:
		return arg.Value == `"git"`
	case *ast.Ident:
		return arg.Name == "git"
	case *ast.SelectorExpr:
		return arg.Sel.Name == "git"
	}
	return false
}

func TestRunsGitRecognizesDirectCalls(t *testing.T) {
	for source, want := range map[string]bool{
		`exec.Command("git", "init")`:                        true,
		`exec.CommandContext(ctx, git, "init")`:              true,
		`exec.CommandContext(ctx, r.git, "init")`:            true,
		`exec.CommandContext(ctx, "go", "test")`:             false,
		`exec.Command(path, "git")`:                          false,
		`testgit.Command(ctx, git, dir, "init")`:             false,
		`exec.CommandContext(ctx, filepath.Join(root, "x"))`: false,
	} {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatal(err)
		}
		if got := runsGit(expr.(*ast.CallExpr)); got != want {
			t.Errorf("%s: runsGit = %v, want %v", source, got, want)
		}
	}
}
