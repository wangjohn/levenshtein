package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

func mutationConfig(check string) string {
	return fmt.Sprintf(`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"},"host":{"executor":"native"}},"checks":{"m":%s},"runs":{"r":{"checks":["m"]}}}`, check)
}

func TestGoMutationOptionsArePlanned(t *testing.T) {
	cfg, err := Parse([]byte(mutationConfig(`{"kind":"go-mutation","target":"app","environment":"go","mutation":{"base":"develop","accepted":"ci/accepted.json","tags":"integration,slow","timeout":"5m"}}`)))
	if err != nil {
		t.Fatal(err)
	}

	plan, err := cfg.Plan(t.TempDir(), "r")
	if err != nil {
		t.Fatal(err)
	}

	got := plan.Checks[0].Check.mutationOptions()
	want := MutationCheck{Base: "develop", Scope: MutationScopeChanged, Accepted: "ci/accepted.json", Tags: "integration,slow", Timeout: "5m"}
	if got != want {
		t.Fatalf("options = %+v, want %+v", got, want)
	}
}

func TestGoMutationDefaults(t *testing.T) {
	got := Check{Kind: CheckGoMutation}.mutationOptions()

	if got.Scope != MutationScopeChanged || got.Accepted != ".levenshtein/mutation-accepted.json" {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestGoMutationRejectsInvalidOptions(t *testing.T) {
	for name, check := range map[string]string{
		"unknown scope":         `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"scope":"diff"}}`,
		"base with module":      `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"scope":"module","base":"main"}}`,
		"base read as option":   `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"base":"--output=x"}}`,
		"escaping accepted":     `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"accepted":"../accepted.json"}}`,
		"private accepted":      `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"accepted":".env"}}`,
		"tags with a space":     `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"tags":"a b"}}`,
		"bad timeout":           `{"kind":"go-mutation","target":"app","environment":"go","mutation":{"timeout":"soon"}}`,
		"options on go-lint":    `{"kind":"go-lint","target":"app","environment":"go","mutation":{}}`,
		"native environment":    `{"kind":"go-mutation","target":"app","environment":"host"}`,
		"command on a mutation": `{"kind":"go-mutation","target":"app","environment":"go","command":{"args":["true"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Parse([]byte(mutationConfig(check)))
			if err == nil {
				_, err = cfg.Plan(t.TempDir(), "r")
			}

			if err == nil {
				t.Fatal("accepted invalid go-mutation configuration")
			}
		})
	}
}

// mutationRepo is a repository whose target module "app" changed in every way
// the host selection has to tell apart.
type mutationRepo struct {
	dir string
	git string
	env []string
}

func (r mutationRepo) run(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), r.git, args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r mutationRepo) write(t *testing.T, path, content string) {
	t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newMutationRepo(t *testing.T) mutationRepo {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Isolate git configuration without changing HOME; Apple's git shim reads its license state from there.
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	for _, entry := range env[len(os.Environ()):] {
		name, value, _ := strings.Cut(entry, "=")
		t.Setenv(name, value) // mutationFiles runs git with the process environment.
	}
	t.Setenv("GITHUB_BASE_REF", "") // CI sets it for pull requests; each test chooses its own base.
	r := mutationRepo{dir: dir, git: git, env: env}
	r.run(t, "init", "--quiet", "--initial-branch=main")

	unchanged := map[string]string{
		"app/go.mod":           "module example.com/app\n",
		"app/keep.go":          "package app\n",
		"app/edited.go":        "package app\n",
		"app/edited_test.go":   "package app\n",
		"app/gen.go":           "package app\n",
		"app/nested/go.mod":    "module example.com/nested\n",
		"app/nested/z.go":      "package nested\n",
		"app/testdata/x.go":    "package x\n",
		"app/vendor/v/y.go":    "package v\n",
		"app/skipped/s.go":     "package skipped\n",
		"other/outside.go":     "package other\n",
		"app/README.md":        "# app\n",
		"app/internal/deep.go": "package internal\n",
	}
	for path, content := range unchanged {
		r.write(t, path, content)
	}
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "base")

	r.run(t, "switch", "--quiet", "-c", "feature")
	for path := range unchanged {
		if path != "app/keep.go" && !strings.HasSuffix(path, "go.mod") {
			r.write(t, path, unchanged[path]+"\n// changed\n")
		}
	}
	r.write(t, "app/gen.go", "// Code generated by hand. DO NOT EDIT.\n\npackage app\n")
	r.run(t, "commit", "--quiet", "-am", "feature")
	r.write(t, "app/untracked.go", "package app\n")
	return r
}

func mutationRequest(dir string, options *MutationCheck) Request {
	return Request{
		Source: dir,
		PlannedCheck: PlannedCheck{
			Check:  Check{Kind: CheckGoMutation, Mutation: options},
			Target: Target{Dir: "app", Inputs: []string{"app"}, Exclude: []string{"app/skipped"}},
		},
	}
}

func TestMutationFilesSelectsTheModulesChangedHandWrittenCode(t *testing.T) {
	r := newMutationRepo(t)

	selection, err := mutationFiles(t.Context(), mutationRequest(r.dir, nil))
	if err != nil {
		t.Fatal(err)
	}

	// Left out: the unchanged file, tests, generated code, a nested module,
	// testdata, vendor, an excluded path, Markdown, and a file outside the target.
	want := []string{"edited.go", "internal/deep.go", "untracked.go"}
	if !slices.Equal(selection.Files, want) {
		t.Fatalf("Files = %v, want %v", selection.Files, want)
	}
	if !strings.Contains(selection.Note, "main") {
		t.Errorf("note %q should name the base", selection.Note)
	}
}

func TestMutationFilesReadsTheBaseFromGitHubWhenUnset(t *testing.T) {
	r := newMutationRepo(t)
	r.run(t, "branch", "release", "main")
	t.Setenv("GITHUB_BASE_REF", "release")

	selection, err := mutationFiles(t.Context(), mutationRequest(r.dir, nil))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(selection.Note, "release") {
		t.Fatalf("note %q should name GITHUB_BASE_REF's branch", selection.Note)
	}
}

func TestMutationFilesModuleScopeTakesEveryEligibleFile(t *testing.T) {
	r := newMutationRepo(t)

	selection, err := mutationFiles(t.Context(), mutationRequest(r.dir, &MutationCheck{Scope: MutationScopeModule}))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"edited.go", "internal/deep.go", "keep.go", "untracked.go"}
	if !slices.Equal(selection.Files, want) {
		t.Fatalf("Files = %v, want %v", selection.Files, want)
	}
}

func TestMutationSkipsDaggerWhenNothingIsMutable(t *testing.T) {
	r := newMutationRepo(t)
	r.run(t, "switch", "--quiet", "main")
	if err := os.Remove(filepath.Join(r.dir, "app", "untracked.go")); err != nil {
		t.Fatal(err)
	}
	runner := &Dagger{}

	result := runner.Execute(t.Context(), mutationRequest(r.dir, nil))

	if result.Status != StatusPassed || runner.client != nil || !strings.Contains(result.Stdout, "no Go files to mutate") {
		t.Fatalf("an empty selection passes without a Dagger session: %+v", result)
	}
}

func TestMutationRejectsAnAcceptedFileTheSourceWouldLeaveOut(t *testing.T) {
	r := newMutationRepo(t)
	r.write(t, ".levenshtein/mutation-accepted.json", `{"version": 1, "accepted": []}`)

	result := (&Dagger{}).Execute(t.Context(), mutationRequest(r.dir, nil))

	if result.Status != StatusError || !strings.Contains(result.Error, "outside the target's inputs") {
		t.Fatalf("an unreachable accepted file must be an error, not an empty list: %+v", result)
	}
}

func TestMutationSelectionErrorNamesTheBase(t *testing.T) {
	r := newMutationRepo(t)

	result := (&Dagger{}).Execute(t.Context(), mutationRequest(r.dir, &MutationCheck{Base: "trunk"}))

	if result.Status != StatusError || !strings.Contains(result.Error, `"trunk"`) {
		t.Fatalf("a missing base is an error naming it: %+v", result)
	}
}

func TestDaggerResultMarksTimedOutMutantsIncomplete(t *testing.T) {
	summary := `{"killed":1,"lived":1,"accepted":0,"not_covered":0,"timed_out":1,"not_viable":0,"skipped":0,"files":["clamp.go"]}`
	err := &gqlerror.Error{Message: "mutation testing failed", Extensions: map[string]any{
		"levenshteinFindings":   []any{map[string]any{"code": "go-mutation-timeout"}},
		"levenshteinIncomplete": true,
		"levenshteinSummary":    summary,
	}}

	result := daggerResult(err)

	if result.Status != StatusIncomplete {
		t.Fatalf("status = %s, want incomplete", result.Status)
	}
	var details struct {
		Findings []json.RawMessage `json:"findings"`
		Summary  mutationSummary   `json:"summary"`
	}
	if json.Unmarshal(result.Details, &details) != nil || len(details.Findings) != 1 || details.Summary.TimedOut != 1 {
		t.Fatalf("details lost the findings or summary: %s", result.Details)
	}
}

func TestMutationStdoutSummarizesAPassingRun(t *testing.T) {
	summary := `{"killed":3,"lived":0,"accepted":1,"not_covered":2,"timed_out":0,"not_viable":0,"skipped":0,"uncovered":[{"file":"a.go","line":4,"column":2,"mutator":"ARITHMETIC_BASE"}],"files":["a.go","b.go"]}`

	stdout, details := mutationStdout(mutationSummaryOf(Result{}, summary), "Go files changed since main", nil)

	if stdout != "Go files changed since main: 2 files mutated; 3 killed, 0 survived, 1 accepted, 2 not covered, 0 timed out" {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(string(details), `"uncovered":[{"file":"a.go"`) {
		t.Errorf("a pass must keep its uncovered list in the details: %s", details)
	}
}
