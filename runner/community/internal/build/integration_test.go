package build

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/runner/community"
)

// pinnedRequest reads the Go and Staticcheck pins from runner/toolchain.json,
// so these tests build exactly what the runner would.
func pinnedRequest(t *testing.T, modules ...Module) Request {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "toolchain.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pins struct {
		Go          string `json:"go"`
		Staticcheck string `json:"staticcheck"`
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return Request{Go: pins.Go, Staticcheck: pins.Staticcheck, Community: dir, Modules: modules}
}

func faultyModule(t *testing.T, namespace string) Module {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "faulty"))
	if err != nil {
		t.Fatal(err)
	}
	return Module{Path: "example.com/lvrules-faulty", Version: "v0.0.0", Namespace: namespace, Dir: dir}
}

// lintRun is one run of a built community linter over testdata/consumer.
type lintRun struct {
	Exit     int
	Stdout   string
	Report   community.Report
	Reported bool
}

func runLinter(t *testing.T, binary, cache string, cfg community.Config) lintRun {
	t.Helper()
	dir := t.TempDir()
	configPath, reportPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "report.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), binary, "-f=json", "-lvrules.config="+configPath, "-lvrules.report="+reportPath, "./...")
	cmd.Dir = filepath.Join("testdata", "consumer")
	cmd.Env = append(os.Environ(), "STATICCHECK_CACHE="+cache, "GOWORK=off", "GOTOOLCHAIN=local")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	err = cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}

	var report community.Report
	data, err = os.ReadFile(reportPath)
	reported := err == nil
	if reported {
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
	}
	return lintRun{Exit: code, Stdout: stdout.String(), Report: report, Reported: reported}
}

// TestFailingRulesAreErrorsEvenFromTheCache builds a real community linter
// from a module whose rules fail on purpose and runs each failing rule twice
// over one Staticcheck cache. Staticcheck alone would swallow the error and
// cache the package as passing, or die on the panic without a report.
func TestFailingRulesAreErrorsEvenFromTheCache(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a linter, which needs the module proxy")
	}
	out := t.TempDir()
	if _, err := Build(context.Background(), pinnedRequest(t, faultyModule(t, "faulty")), t.TempDir(), out); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(out, Binary)
	cache := t.TempDir()

	for _, test := range []struct {
		rule  string
		error string
	}{
		{"faulty_oops", "oops failed on purpose"},
		{"faulty_boom", "panic: boom failed on purpose"},
	} {
		cfg := community.Config{Modules: []community.ModuleConfig{{
			Path: "example.com/lvrules-faulty", Version: "v0.0.0", Namespace: "faulty", Select: []string{"faulty_ok", test.rule},
		}}}
		want := []community.Failure{{Code: test.rule, Source: "example.com/lvrules-faulty@v0.0.0", Package: "example.com/consumer", Error: test.error}}

		for _, attempt := range []string{"first", "second"} {
			run := runLinter(t, binary, cache, cfg)

			if !run.Reported || run.Exit != 4 {
				t.Fatalf("%s %s run: exit %d, reported %v", test.rule, attempt, run.Exit, run.Reported)
			}
			if !slices.Equal(run.Report.Failures, want) {
				t.Errorf("%s %s run: failures = %+v, want %+v", test.rule, attempt, run.Report.Failures, want)
			}
		}
	}
}

func TestAHealthyRunCachesAndSelectsOnlyWhatTheCheckAsks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a linter, which needs the module proxy")
	}
	out := t.TempDir()
	if _, err := Build(context.Background(), pinnedRequest(t, faultyModule(t, "faulty")), t.TempDir(), out); err != nil {
		t.Fatal(err)
	}
	cfg := community.Config{
		Modules: []community.ModuleConfig{{Path: "example.com/lvrules-faulty", Version: "v0.0.0", Namespace: "faulty", Select: []string{"faulty_*"}, Advisory: []string{"faulty_ok"}}},
		Checks:  []string{"-faulty_boom", "-faulty_oops"},
	}

	run := runLinter(t, filepath.Join(out, Binary), t.TempDir(), cfg)

	if !run.Reported || len(run.Report.Failures) != 0 {
		t.Fatalf("report = %+v (exit %d)", run.Report, run.Exit)
	}
	// faulty_ok is advisory, but the renamed directive still fails the run.
	if run.Exit != 1 || !strings.Contains(run.Stdout, `"code":"faulty_ok","severity":"warning"`) {
		t.Errorf("exit %d, output:\n%s", run.Exit, run.Stdout)
	}
}

func TestANamespaceMismatchFailsTheBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a linter, which needs the module proxy")
	}

	_, err := Build(context.Background(), pinnedRequest(t, faultyModule(t, "errs")), t.TempDir(), t.TempDir())

	want := `levenshtein.json declares namespace "errs" for example.com/lvrules-faulty@v0.0.0, but its lvrules.Namespace is "faulty"`
	if err == nil || err.Error() != want {
		t.Errorf("got %v\nwant %s", err, want)
	}
}

// proxyWork is an empty generated-module directory, the way Build starts.
func proxyWork(t *testing.T) goCommand {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+generatedModule+"\n\ngo 1.27.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return goCommand{Dir: dir}
}

// Every other build test replaces its module with a directory; this one
// reads published versions through the module proxy, the way a consumer's
// pins are read. gocognit, a core linter dependency, retracted v1.1.1.
func TestPublishedPinsAreReadThroughTheProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the module proxy")
	}
	g := proxyWork(t)
	current := Module{Path: "github.com/uudashr/gocognit", Version: "v1.2.1"}
	retracted := Module{Path: "github.com/uudashr/gocognit", Version: "v1.1.1"}

	if err := checkGoVersion(t.Context(), g, "1.27.1", current); err != nil {
		t.Errorf("a published module within the pinned Go must pass: %v", err)
	}
	if err := checkGoVersion(t.Context(), g, "1.10", current); err == nil || !strings.Contains(err.Error(), "but this release pins go 1.10") {
		t.Errorf("a module newer than the pinned Go must be refused: %v", err)
	}
	missing := Module{Path: "github.com/uudashr/gocognit", Version: "v9.9.9"}
	if err := checkGoVersion(t.Context(), g, "1.27.1", missing); err == nil || !strings.Contains(err.Error(), "downloading the module failed") {
		t.Errorf("a version the proxy does not have must be refused: %v", err)
	}

	found, err := retractions(t.Context(), g, []Module{current, retracted})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Version != "v1.1.1" || !slices.Equal(found[0].Rationale, []string{"Accidentally published."}) {
		t.Errorf("retractions = %+v", found)
	}
}

// A module without an lvrules package is named as such, not reported as a
// generic resolution failure.
func TestAModuleWithoutLvrulesIsNamed(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a linter, which needs the module proxy")
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "nolvrules"))
	if err != nil {
		t.Fatal(err)
	}
	pin := Module{Path: "example.com/lvrules-none", Version: "v0.0.0", Namespace: "none", Dir: dir}

	_, err = Build(t.Context(), pinnedRequest(t, pin), t.TempDir(), t.TempDir())

	if err == nil || err.Error() != "example.com/lvrules-none@v0.0.0 has no lvrules package; see docs/community-rules.md#the-contract" {
		t.Errorf("got %v", err)
	}
}

func TestAnIncompleteGoModFailsTheBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a linter, which needs the module proxy")
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "incomplete"))
	if err != nil {
		t.Fatal(err)
	}
	pin := Module{Path: "example.com/lvrules-incomplete", Version: "v0.0.0", Namespace: "incomplete", Dir: dir}

	_, err = Build(t.Context(), pinnedRequest(t, pin), t.TempDir(), t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "imports a package that no go.mod requires") || !strings.Contains(err.Error(), "rsc.io/quote") {
		t.Errorf("got %v", err)
	}
}
