package verify

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// historyRepo is a git repository for checks that read history: its main
// branch holds base and the checked-out feature branch holds head, both as
// maps from slash-separated paths to contents, with a nil content deleting a
// file. After the branch point main moves on, so the merge base is not main's
// tip.
func historyRepo(t *testing.T, base, head map[string]*string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// apidiffBase runs git with the process environment, so isolate it there.
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1", "GITHUB_BASE_REF": "",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(name, value)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), git, args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	apply := func(files map[string]*string) {
		t.Helper()
		for name, content := range files {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if content == nil {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				continue
			}
			writeTestFile(t, path, *content)
		}
	}

	run("init", "--quiet", "--initial-branch=main")
	apply(base)
	writeTestFile(t, filepath.Join(dir, "README"), "base\n")
	run("add", "--all")
	run("commit", "--quiet", "-m", "base")
	run("branch", "feature")
	writeTestFile(t, filepath.Join(dir, "README"), "main moved on\n")
	run("commit", "--quiet", "-am", "later on main")
	run("switch", "--quiet", "feature")
	apply(head)
	run("add", "--all")
	run("commit", "--quiet", "--allow-empty", "-m", "feature")
	return dir
}

func text(content string) *string {
	return &content
}

func apidiffRequest(source string, target Target) Request {
	return Request{
		Source: source,
		PlannedCheck: PlannedCheck{
			ID:          "api",
			Check:       Check{Kind: CheckGoApidiff, Target: "lib", Environment: "host"},
			Target:      target,
			Environment: Environment{Executor: ExecutorNative},
		},
	}
}

// The base tree is the target's declared inputs at the merge base: not main's
// tip, not the branch, and without excludes, private files, or anything the
// inputs do not cover.
func TestApidiffBaseExportsTheInputsAtTheMergeBase(t *testing.T) {
	dir := historyRepo(t, map[string]*string{
		"lib/go.mod":    text("module example.com/lib\n"),
		"lib/a.go":      text("package lib // base\n"),
		"lib/.env":      text("TOKEN=secret\n"),
		"lib/skip/x.go": text("package skip\n"),
		"other/o.go":    text("package other\n"),
	}, map[string]*string{
		"lib/a.go":   text("package lib // branch\n"),
		"lib/new.go": text("package lib\n"),
	})
	req := apidiffRequest(dir, Target{Dir: "lib", Inputs: []string{"lib", "missing"}, Exclude: []string{"lib/skip"}})

	base, release, err := apidiffBase(t.Context(), req)
	defer release()
	if err != nil {
		t.Fatal(err)
	}
	if base.Ref != "main" || base.Missing || len(base.Commit) < 40 {
		t.Fatalf("base = %+v", base)
	}
	if got, want := listTree(t, base.Dir), []string{filepath.Join("lib", "a.go"), filepath.Join("lib", "go.mod")}; !slices.Equal(got, want) {
		t.Fatalf("exported %v, want %v", got, want)
	}
	if data, err := os.ReadFile(filepath.Join(base.Dir, "lib", "a.go")); err != nil || string(data) != "package lib // base\n" {
		t.Fatalf("lib/a.go at the merge base = %q %v", data, err)
	}
	release()
	if _, err := os.Stat(base.Dir); !os.IsNotExist(err) {
		t.Fatalf("release must remove the export: %v", err)
	}

	req.Check.Apidiff = &ApidiffCheck{Base: "trunk"}
	if _, _, err := apidiffBase(t.Context(), req); err == nil || !strings.Contains(err.Error(), `base branch "trunk" was not found`) {
		t.Fatalf("an unknown base branch must be an error: %v", err)
	}
}

// A source inside the work tree maps git's top-level paths onto itself, and a
// module the base did not have is reported missing rather than compared.
func TestApidiffBaseFollowsTheSourceAndNoticesANewModule(t *testing.T) {
	dir := historyRepo(t, map[string]*string{
		"repo/old/go.mod": text("module example.com/old\n"),
	}, map[string]*string{
		"repo/lib/go.mod": text("module example.com/lib\n"),
		"repo/old/a.go":   text("package old\n"),
	})
	source := filepath.Join(dir, "repo")

	fresh, release, err := apidiffBase(t.Context(), apidiffRequest(source, Target{Dir: "lib", Inputs: []string{"."}}))
	defer release()
	if err != nil || !fresh.Missing || fresh.Dir != "" {
		t.Fatalf("a module missing at the base must be reported, not exported: %+v %v", fresh, err)
	}

	old, releaseOld, err := apidiffBase(t.Context(), apidiffRequest(source, Target{Dir: "old", Inputs: []string{"."}}))
	defer releaseOld()
	if err != nil || old.Missing {
		t.Fatalf("old existed at the base: %+v %v", old, err)
	}
	if got := listTree(t, old.Dir); !slices.Equal(got, []string{filepath.Join("old", "go.mod")}) {
		t.Fatalf("the export must be relative to the source: %v", got)
	}
}

// The base is exported from the declared inputs, so inputs without the
// target's go.mod would make an existing module look new and pass.
func TestApidiffBaseRequiresTheModuleFileAmongTheInputs(t *testing.T) {
	dir := historyRepo(t, map[string]*string{
		"repo/lib/go.mod": text("module example.com/lib\n"),
		"repo/lib/a.go":   text("package lib\n\nfunc A() {}\n"),
	}, map[string]*string{
		"repo/lib/a.go": text("package lib\n"),
	})
	source := filepath.Join(dir, "repo")

	for name, target := range map[string]Target{
		"not declared": {Dir: "lib", Inputs: []string{filepath.Join("lib", "a.go")}},
		"excluded":     {Dir: "lib", Inputs: []string{"lib"}, Exclude: []string{filepath.Join("lib", "go.mod")}},
	} {
		base, release, err := apidiffBase(t.Context(), apidiffRequest(source, target))
		release()
		if err == nil || !strings.Contains(err.Error(), "go.mod among its inputs") {
			t.Errorf("%s: a target without its go.mod must be an error, not a new module: %+v %v", name, base, err)
		}
	}
}

// Neither executor reuses a go-apidiff result, since where the base branch
// points is outside every fingerprint; Dagger still answers an identical
// call from its own cache unless the run is fresh.
func TestApidiffResultsAreNeverReusedByTheCLI(t *testing.T) {
	for _, executor := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		req := cacheRequest(t)
		req.Check.Kind = CheckGoApidiff
		req.Environment.Executor = executor
		counting := &countingExecutor{status: StatusPassed}
		runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: counting}
		for range 2 {
			if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheDisabled || !strings.Contains(result.Cache.Reason, "base branch") {
				t.Fatalf("%s: go-apidiff must not be cached: %+v", executor, result.Cache)
			}
		}
		if counting.calls != 2 {
			t.Fatalf("%s: both runs must execute, got %d", executor, counting.calls)
		}
	}

	req := cacheRequest(t)
	req.Check.Kind = CheckGoApidiff
	if executionNonce(req) != "" {
		t.Fatal("an ordinary go-apidiff run should let Dagger reuse an identical call")
	}
	req.RerunChecks = true
	if executionNonce(req) == "" {
		t.Fatal("a fresh go-apidiff run must get unique Dagger execution inputs")
	}
}

func TestApidiffOptionsBelongToGoApidiff(t *testing.T) {
	for _, env := range []Environment{nativeGoEnvironment(), {Executor: ExecutorDagger}} {
		if err := validateCheck(Check{Kind: CheckGoApidiff, Apidiff: &ApidiffCheck{Base: "release/v1"}}, env); err != nil {
			t.Errorf("%s: rejected a base branch: %v", env.Executor, err)
		}
		if err := validateCheck(Check{Kind: CheckGoApidiff}, env); err != nil {
			t.Errorf("%s: go-apidiff needs no options: %v", env.Executor, err)
		}
		for _, base := range []string{"-main", "two words"} {
			if err := validateCheck(Check{Kind: CheckGoApidiff, Apidiff: &ApidiffCheck{Base: base}}, env); err == nil {
				t.Errorf("%s: accepted base %q", env.Executor, base)
			}
		}
		if err := validateCheck(Check{Kind: CheckGoVet, Apidiff: &ApidiffCheck{}}, env); err == nil || !strings.Contains(err.Error(), "only to go-apidiff") {
			t.Errorf("%s: go-vet accepted apidiff options: %v", env.Executor, err)
		}
	}
}

// Both executors report a verdict the same way: the comparison and notes as
// output, and a summary beside any findings. A run without a verdict keeps
// what the tool said.
func TestApidiffResultNamesTheComparison(t *testing.T) {
	base := apidiffBaseTree{Ref: "origin/main", Commit: "0123456789abcdef0123456789abcdef01234567"}
	findings := findingsDetails([]finding{{Code: string(CheckGoApidiff), Message: "incompatible", Location: location{File: "a.go", Line: 3}}})

	failed := apidiffResult(Result{Status: StatusFailed, Details: findings}, []string{"1 incompatible", "compatible: F: added"}, base)
	var details struct {
		Findings []finding      `json:"findings"`
		Summary  apidiffSummary `json:"summary"`
	}
	if err := json.Unmarshal(failed.Details, &details); err != nil {
		t.Fatal(err)
	}
	if len(details.Findings) != 1 || details.Summary.Base != "origin/main" || details.Summary.MergeBase != base.Commit || !slices.Contains(details.Summary.Notes, "compatible: F: added") {
		t.Fatalf("details = %+v", details)
	}
	if !strings.HasPrefix(failed.Stdout, "compared with the merge base of origin/main, 0123456789ab\n") {
		t.Fatalf("stdout = %q", failed.Stdout)
	}

	passed := apidiffResult(Result{Status: StatusPassed}, nil, base)
	if !strings.Contains(string(passed.Details), `"merge_base"`) || strings.Contains(string(passed.Details), `"findings"`) {
		t.Fatalf("a pass carries the summary alone: %s", passed.Details)
	}
	broken := apidiffResult(Result{Status: StatusError, Error: "apidiff exited 1", Stdout: "raw"}, nil, base)
	if broken.Stdout != "raw" || broken.Details != nil {
		t.Fatalf("an error must keep the tool's output: %+v", broken)
	}
}

// The base is the committed tree as it was, not a release archive of it:
// export-ignore and export-subst attributes must not drop a package or
// rewrite a file, or a breaking change to it would look like an addition.
func TestApidiffBaseIgnoresArchiveAttributes(t *testing.T) {
	dir := historyRepo(t, map[string]*string{
		".gitattributes":   text("lib/contrib export-ignore\nlib/version.go export-subst\n"),
		"lib/go.mod":       text("module example.com/lib\n"),
		"lib/contrib/c.go": text("package contrib\n\nfunc F() {}\n"),
		"lib/version.go":   text("package lib\n\nconst Version = \"$Format:%H$\"\n"),
	}, map[string]*string{
		"lib/contrib/c.go": text("package contrib\n"),
	})
	req := apidiffRequest(dir, Target{Dir: "lib", Inputs: []string{"lib"}})

	base, release, err := apidiffBase(t.Context(), req)
	defer release()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{filepath.Join("lib", "contrib", "c.go"), filepath.Join("lib", "go.mod"), filepath.Join("lib", "version.go")}
	if got := listTree(t, base.Dir); !slices.Equal(got, want) {
		t.Fatalf("exported %v, want %v", got, want)
	}
	if data, err := os.ReadFile(filepath.Join(base.Dir, "lib", "version.go")); err != nil || !strings.Contains(string(data), "$Format:%H$") {
		t.Fatalf("lib/version.go was rewritten: %q %v", data, err)
	}
}
