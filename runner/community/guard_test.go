package community

import (
	"errors"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func passFor(analyzer *analysis.Analyzer, pkg string) *analysis.Pass {
	return &analysis.Pass{Analyzer: analyzer, Pkg: types.NewPackage(pkg, filepath.Base(pkg))}
}

func TestGuardRecordsErrorsAndPanicsAcrossTheRequiresGraph(t *testing.T) {
	dependency := &analysis.Analyzer{Name: "helper", Run: func(*analysis.Pass) (any, error) { panic("index out of range") }}
	rule := &analysis.Analyzer{Name: "errs_nopanic", Requires: []*analysis.Analyzer{dependency}, Run: func(*analysis.Pass) (any, error) {
		return nil, errors.New("no type information")
	}}
	calls := 0
	g := newGuard(func() { calls++ })

	g.wrap(rule, owner{Code: "errs_nopanic", Source: errsPath + "@v1.4.0"})
	_, ruleErr := rule.Run(passFor(rule, "example.com/b"))
	_, _ = rule.Run(passFor(rule, "example.com/a"))
	_, dependencyErr := dependency.Run(passFor(dependency, "example.com/a"))

	if ruleErr == nil || dependencyErr == nil || !strings.Contains(dependencyErr.Error(), "panic: index out of range") {
		t.Fatalf("the guard must hand Staticcheck the failure as an error: %v, %v", ruleErr, dependencyErr)
	}
	if calls != 1 {
		t.Errorf("onFail ran %d times, want once", calls)
	}
	want := []Failure{
		{Code: "errs_nopanic", Source: errsPath + "@v1.4.0", Packages: []string{"example.com/a", "example.com/b"}, Error: "no type information"},
		{Code: "helper", Source: errsPath + "@v1.4.0", Packages: []string{"example.com/a"}, Error: "panic: index out of range"},
	}
	got := g.report()
	if len(got) != len(want) {
		t.Fatalf("failures = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Code != want[i].Code || got[i].Source != want[i].Source || strings.Join(got[i].Packages, ",") != strings.Join(want[i].Packages, ",") || got[i].Error != want[i].Error {
			t.Errorf("failure %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGuardWrapsAnAnalyzerOnce(t *testing.T) {
	calls := 0
	shared := &analysis.Analyzer{Name: "inspect", Run: func(*analysis.Pass) (any, error) { calls++; return nil, nil }}
	g := newGuard(func() {})

	g.wrap(shared, owner{Code: "inspect"})
	g.wrap(shared, owner{Code: "inspect"})
	_, err := shared.Run(passFor(shared, "example.com/a"))

	if err != nil || calls != 1 {
		t.Errorf("a doubly wrapped analyzer ran %d times: %v", calls, err)
	}
}

func TestCacheGenerationMovesForwardAfterAFailure(t *testing.T) {
	base := t.TempDir()
	t.Setenv("STATICCHECK_CACHE", base)

	retire, err := cacheGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("STATICCHECK_CACHE"); got != filepath.Join(base, "gen-0") {
		t.Fatalf("STATICCHECK_CACHE = %q, want generation 0", got)
	}
	retire()
	retire()

	t.Setenv("STATICCHECK_CACHE", base)
	if _, err := cacheGeneration(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("STATICCHECK_CACHE"); got != filepath.Join(base, "gen-1") {
		t.Errorf("after a failure the next run must use generation 1, got %q", got)
	}
}

func TestCacheGenerationLeavesAnExplicitOffAlone(t *testing.T) {
	t.Setenv("STATICCHECK_CACHE", "off")

	if _, err := cacheGeneration(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("STATICCHECK_CACHE"); got != "off" {
		t.Errorf("STATICCHECK_CACHE = %q", got)
	}
}

func TestCacheGenerationRefusesACorruptMarker(t *testing.T) {
	base := t.TempDir()
	t.Setenv("STATICCHECK_CACHE", base)
	if err := os.WriteFile(filepath.Join(base, generationFile), []byte("latest"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := cacheGeneration()

	if err == nil || !strings.Contains(err.Error(), "not a cache generation") {
		t.Errorf("got %v", err)
	}
}

func TestOldCacheGenerationsArePruned(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"gen-0", "gen-1", "gen-2", "gen-3", "unrelated"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, generationFile), []byte("3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STATICCHECK_CACHE", base)

	if _, err := cacheGeneration(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "gen-2,gen-3,"+generationFile+",unrelated" {
		t.Errorf("left %v; want the current and previous generations and nothing else removed", names)
	}
}
