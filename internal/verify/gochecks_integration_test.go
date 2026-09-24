//go:build integration

package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// fixtureRequest points a native shared Go check at one of the runner's own
// fixture modules, with this repository as the pinned shared checkout.
func fixtureRequest(t *testing.T, shared, fixture string, kind CheckKind) Request {
	t.Helper()
	source, err := filepath.EvalSymlinks(filepath.Join(shared, "runner", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}

	return Request{
		Source: source,
		Shared: shared,
		PlannedCheck: PlannedCheck{
			ID:          string(kind),
			Check:       Check{Kind: kind, Target: "app", Environment: "host"},
			Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}},
			Environment: Environment{Executor: ExecutorNative},
		},
	}
}

// fixtureFiles reads a fixture tree into the shape historyRepo takes, under
// prefix.
func fixtureFiles(t *testing.T, root, prefix string) map[string]*string {
	t.Helper()
	files := map[string]*string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		files[prefix+filepath.ToSlash(rel)] = text(string(data))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func fixtureFindings(t *testing.T, result Result) []finding {
	t.Helper()
	var details struct {
		Findings []finding `json:"findings"`
	}
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatalf("findings are not the Dagger path's shape: %v (%s)", err, result.Details)
	}
	return details.Findings
}

// The native executor has to reach the same verdicts as the pinned container on
// the fixtures the Dagger self-test uses, including the exact rule codes.
func TestNativeGoChecksAgreeWithTheFixtures(t *testing.T) {
	shared := repositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	native := &Native{Cache: &Cache{Dir: t.TempDir()}}

	t.Run("go-lint passes good code", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckGoLint))
		if result.Status != StatusPassed {
			t.Fatalf("good fixture must pass: %+v", result)
		}
	})

	t.Run("go-lint fails bad code with its rule codes", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "bad", CheckGoLint))
		if result.Status != StatusFailed {
			t.Fatalf("bad fixture must fail for diagnostics, not a tool error: %+v", result)
		}

		counts := map[string]int{}
		for _, item := range fixtureFindings(t, result) {
			counts[item.Code]++
			if filepath.IsAbs(item.Location.File) || item.Location.Line < 1 {
				t.Errorf("finding location is not repository-relative: %+v", item.Location)
			}
		}
		for _, code := range []string{"SA5001", "SA5003", "SA9001", "LV1001", "LV1002", "errcheck", "exhaustive"} {
			if counts[code] < 1 {
				t.Errorf("bad fixture must produce a %s diagnostic; got %v", code, counts)
			}
		}
	})

	t.Run("go-lint refuses an empty module", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "empty", CheckGoLint))
		if result.Status != StatusError {
			t.Fatalf("empty module must error rather than pass: %+v", result)
		}
	})

	t.Run("go-lint leaves opt-in gocognit off by default", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "complexity", CheckGoLint))
		if result.Status != StatusPassed {
			t.Fatalf("complexity fixture must pass the shipped selection: %+v", result)
		}
	})

	t.Run("go-lint reports gocognit when the check adds it", func(t *testing.T) {
		req := fixtureRequest(t, shared, "complexity", CheckGoLint)
		req.Check.Lint = &LintCheck{Checks: []string{"gocognit"}}
		result := native.Execute(ctx, req)
		if result.Status != StatusFailed {
			t.Fatalf("complexity fixture must fail once gocognit is added: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != "gocognit" || findings[0].Location.File != "complexity.go" {
			t.Fatalf("want one gocognit finding in complexity.go, got %+v", findings)
		}
	})

	t.Run("go-lint drops a default rule the check turns off", func(t *testing.T) {
		req := fixtureRequest(t, shared, "bad", CheckGoLint)
		req.Check.Lint = &LintCheck{Checks: []string{"-errcheck"}}
		result := native.Execute(ctx, req)
		if result.Status != StatusFailed {
			t.Fatalf("bad fixture must still fail for its other rules: %+v", result)
		}

		counts := map[string]int{}
		for _, item := range fixtureFindings(t, result) {
			counts[item.Code]++
		}
		if counts["errcheck"] != 0 || counts["SA5001"] < 1 {
			t.Fatalf("want errcheck off and the rest on; got %v", counts)
		}
	})

	t.Run("go-lint refuses an added rule the linter does not register", func(t *testing.T) {
		req := fixtureRequest(t, shared, "complexity", CheckGoLint)
		req.Check.Lint = &LintCheck{Checks: []string{"gocogint"}}
		result := native.Execute(ctx, req)
		if result.Status != StatusError || !strings.Contains(result.Error, `"gocogint" matches no rule`) {
			t.Fatalf("a misspelled rule must be an error, not a silent pass: %+v", result)
		}
	})

	t.Run("go-vet fails vet-bad", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "vet-bad", CheckGoVet))
		if result.Status != StatusFailed {
			t.Fatalf("vet-bad fixture must fail: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckGoVet) || findings[0].Message == "" {
			t.Fatalf("lost the vet diagnostic: %+v", findings)
		}
	})

	t.Run("go-vet passes good code", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckGoVet))
		if result.Status != StatusPassed {
			t.Fatalf("good fixture must pass vet: %+v", result)
		}
	})

	t.Run("go-mod fails mod-untidy with tidy's diff", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "mod-untidy", CheckGoMod))
		if result.Status != StatusFailed {
			t.Fatalf("mod-untidy fixture must fail for its diff, not a tool error: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckGoMod) || !strings.Contains(findings[0].Message, "-require example.com/mod-untidy/unused v0.0.0") {
			t.Fatalf("lost the tidy diff: %+v", findings)
		}
	})

	// A module that declares no packages, like one that only pins tools, still
	// has manifests to check.
	for _, fixture := range []string{"mod-tidy", "empty"} {
		t.Run("go-mod passes "+fixture, func(t *testing.T) {
			result := native.Execute(ctx, fixtureRequest(t, shared, fixture, CheckGoMod))
			if result.Status != StatusPassed {
				t.Fatalf("%s fixture must pass go-mod: %+v", fixture, result)
			}
		})
	}

	t.Run("go-mod refuses a directory without go.mod", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "mutation", CheckGoMod))
		if result.Status != StatusError || !strings.Contains(result.Error, "needs a readable go.mod") {
			t.Fatalf("a missing go.mod must error rather than pass: %+v", result)
		}
	})

	t.Run("go-test passes test-pass under the race detector", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "test-pass", CheckGoTest))
		if result.Status != StatusPassed {
			t.Fatalf("test-pass fixture must pass go-test: %+v", result)
		}
	})

	for _, fixture := range []struct {
		name    string
		message string
	}{
		{"test-fail", "Add(2, 2) = 4, want 5"},
		{"test-race", "WARNING: DATA RACE"},
	} {
		t.Run("go-test fails "+fixture.name+" with go test's output", func(t *testing.T) {
			result := native.Execute(ctx, fixtureRequest(t, shared, fixture.name, CheckGoTest))
			if result.Status != StatusFailed {
				t.Fatalf("%s fixture must fail for its test, not a tool error: %+v", fixture.name, result)
			}

			findings := fixtureFindings(t, result)
			if len(findings) != 1 || findings[0].Code != string(CheckGoTest) || !strings.Contains(findings[0].Message, fixture.message) {
				t.Fatalf("lost go test's output: %+v", findings)
			}
		})
	}

	t.Run("go-test refuses test-build, whose tests never ran", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "test-build", CheckGoTest))
		if result.Status != StatusError || !strings.Contains(result.Error, "go test could not build example.com/test-build") || !strings.Contains(result.Error, "cannot use Add(1, 2)") {
			t.Fatalf("a test that does not compile must error rather than fail or pass: %+v", result)
		}
	})

	t.Run("go-test refuses a module without tests", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "mod-tidy", CheckGoTest))
		if result.Status != StatusError || !strings.Contains(result.Error, "ran no tests") {
			t.Fatalf("a module with no tests must error rather than pass: %+v", result)
		}
	})

	t.Run("go-imports passes imports-good and fails imports-bad at each import", func(t *testing.T) {
		table := loadImportRuleTable(t)
		good := fixtureRequest(t, shared, "imports-good", CheckGoImports)
		good.Check.Imports = &table.Fixture
		if result := native.Execute(ctx, good); result.Status != StatusPassed {
			t.Fatalf("imports-good must pass go-imports: %+v", result)
		}

		bad := fixtureRequest(t, shared, "imports-bad", CheckGoImports)
		bad.Check.Imports = &table.Fixture
		result := native.Execute(ctx, bad)
		if result.Status != StatusFailed {
			t.Fatalf("imports-bad must fail for its imports, not a tool error: %+v", result)
		}
		var got []string
		for _, finding := range fixtureFindings(t, result) {
			if finding.Code != string(CheckGoImports) {
				t.Errorf("wrong code: %+v", finding)
			}
			got = append(got, fmt.Sprintf("%s:%d", finding.Location.File, finding.Location.Line))
		}
		if want := []string{"core/core.go:7", "core/core_test.go:4", "store/store.go:5"}; !slices.Equal(got, want) {
			t.Fatalf("imports-bad findings = %v, want %v", got, want)
		}
	})

	t.Run("go-imports refuses a package pattern that matches nothing", func(t *testing.T) {
		req := fixtureRequest(t, shared, "imports-good", CheckGoImports)
		req.Check.Imports = &ImportsCheck{Rules: []ImportRule{{Packages: []string{"./cor/..."}, Deny: []string{"net/http"}, Reason: "typo"}}}
		if result := native.Execute(ctx, req); result.Status != StatusError || !strings.Contains(result.Error, `"./cor/..." matches no package`) {
			t.Fatalf("a misspelled package pattern must be an error: %+v", result)
		}
	})

	t.Run("go-generate passes generate-fresh without touching the source", func(t *testing.T) {
		req := fixtureRequest(t, shared, "generate-fresh", CheckGoGenerate)
		result := native.Execute(ctx, req)
		if result.Status != StatusPassed || !strings.Contains(result.Stdout, "ran 1 directive and changed 0 files") {
			t.Fatalf("generate-fresh must pass go-generate after running its directive: %+v", result)
		}
		if _, err := os.Stat(filepath.Join(req.Source, "build")); !os.IsNotExist(err) {
			t.Fatalf("go-generate wrote into the source instead of a copy: %v", err)
		}
	})

	t.Run("go-generate fails generate-stale with the diff", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "generate-stale", CheckGoGenerate))
		if result.Status != StatusFailed {
			t.Fatalf("generate-stale must fail for its stale file, not a tool error: %+v", result)
		}
		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckGoGenerate) || findings[0].Location.File != "names_gen.go" || findings[0].Location.Line != 6 || !strings.Contains(findings[0].Message, "+\t\"blue\",") {
			t.Fatalf("lost the stale file's location or diff: %+v", findings)
		}
	})

	// go-apidiff compares branches, so each case is a repository whose main
	// branch holds apidiff/base and whose feature branch holds a head fixture.
	apidiffCase := func(t *testing.T, head string) Result {
		t.Helper()
		fixtures := filepath.Join(shared, "runner", "testdata", "apidiff")
		files := fixtureFiles(t, filepath.Join(fixtures, head), "lib/")
		for name := range fixtureFiles(t, filepath.Join(fixtures, "base"), "lib/") {
			if _, kept := files[name]; !kept {
				files[name] = nil
			}
		}
		dir := historyRepo(t, fixtureFiles(t, filepath.Join(fixtures, "base"), "lib/"), files)
		req := apidiffRequest(dir, Target{Dir: "lib", Workspace: ".", Inputs: []string{"lib"}})
		req.Shared = shared
		return native.Execute(ctx, req)
	}

	t.Run("go-apidiff fails a breaking branch at each changed declaration", func(t *testing.T) {
		result := apidiffCase(t, "breaking")
		if result.Status != StatusFailed || !strings.HasPrefix(result.Stdout, "compared with the merge base of main, ") {
			t.Fatalf("breaking must fail for its changes, not a tool error: %+v", result)
		}
		var got []string
		for _, finding := range fixtureFindings(t, result) {
			if finding.Code != string(CheckGoApidiff) {
				t.Errorf("wrong code: %+v", finding)
			}
			got = append(got, fmt.Sprintf("%s:%d", finding.Location.File, finding.Location.Line))
		}
		if want := []string{"lib/shapes.go:3", "lib/shapes.go:11", "lib/units/units.go:9"}; !slices.Equal(got, want) {
			t.Fatalf("breaking findings = %v, want %v", got, want)
		}
	})

	t.Run("go-apidiff passes a compatible branch and lists the addition", func(t *testing.T) {
		result := apidiffCase(t, "compatible")
		var details struct {
			Summary apidiffSummary `json:"summary"`
		}
		if err := json.Unmarshal(result.Details, &details); err != nil {
			t.Fatal(err)
		}
		if result.Status != StatusPassed || details.Summary.Base != "main" || !slices.Contains(details.Summary.Notes, "compatible: Circle: added") {
			t.Fatalf("compatible must pass with its addition in the details: %+v", result)
		}
	})

	t.Run("go-apidiff passes a module the base did not have", func(t *testing.T) {
		dir := historyRepo(t, map[string]*string{"other/go.mod": text("module example.com/other\n")}, fixtureFiles(t, filepath.Join(shared, "runner", "testdata", "apidiff", "base"), "lib/"))
		req := apidiffRequest(dir, Target{Dir: "lib", Workspace: ".", Inputs: []string{"lib"}})
		req.Shared = shared
		if result := native.Execute(ctx, req); result.Status != StatusPassed || !strings.Contains(result.Stdout, "did not exist at the base") {
			t.Fatalf("a new module has no API to break: %+v", result)
		}
	})

	// workflow-security downloads the pinned zizmor release, so these need
	// github.com, as the Dagger self-test does.
	t.Run("workflow-security passes workflow-secure", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "workflow-secure", CheckWorkflowSecurity))
		if result.Status != StatusPassed {
			t.Fatalf("workflow-secure fixture must pass workflow-security: %+v", result)
		}
	})

	t.Run("workflow-security fails workflow-insecure with zizmor's report", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "workflow-insecure", CheckWorkflowSecurity))
		if result.Status != StatusFailed {
			t.Fatalf("workflow-insecure fixture must fail for its finding, not a tool error: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckWorkflowSecurity) || !strings.Contains(findings[0].Message, "template-injection") || !strings.Contains(findings[0].Message, ".github/workflows/triage.yml:17") {
			t.Fatalf("lost zizmor's template-injection finding: %+v", findings)
		}
	})

	t.Run("workflow-security refuses a repository with nothing to audit", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckWorkflowSecurity))
		if result.Status != StatusError || !strings.Contains(result.Error, "found no workflows") {
			t.Fatalf("nothing to audit must error rather than pass: %+v", result)
		}
	})
}
