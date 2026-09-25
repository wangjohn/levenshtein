package verify

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// gocognit and deferInLoop are compiled into the linter but opt-in, so the
// shipped selection a native go-lint reads must keep them off while leaving
// the rest on.
func TestShippedSelectionLeavesOptInRulesOff(t *testing.T) {
	t.Parallel()
	checks, err := sharedChecks(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	for _, code := range []string{"gocognit", "deferInLoop"} {
		if checktool.Allowed(checks, code) {
			t.Errorf("the shipped selection %v reports %s", checks, code)
		}
	}
	if !checktool.Allowed(checks, "errcheck") {
		t.Errorf("the shipped selection %v drops errcheck", checks)
	}
}

// A location outside the source root, or one a tool already reported
// relatively, is left exactly as the tool wrote it.
func TestRepositoryPathOnlyRelativizesInsideTheSource(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		root string
		file string
		want string
	}{
		{root: "/src", file: "/src/pkg/a.go", want: "pkg/a.go"},
		{root: "/src", file: "pkg/a.go", want: "pkg/a.go"},
		{root: "/src", file: "/elsewhere/a.go", want: "/elsewhere/a.go"},
		{root: "", file: "/src/a.go", want: "/src/a.go"},
	} {
		if got := repositoryPath(tt.root, tt.file); got != tt.want {
			t.Errorf("repositoryPath(%q, %q) = %q, want %q", tt.root, tt.file, got, tt.want)
		}
	}
}

// A native finding carries everything checktool reported, so the executors
// report the same thing, and an error or a pass carries no findings.
func TestToolFindingsKeepEveryField(t *testing.T) {
	t.Parallel()
	found := checktool.Finding{Code: "errs_sentinel", Message: "m", Location: location{File: "a.go", Line: 2, Column: 3}, Source: "example.com/lvrules@v1.0.0", URL: "https://example.com/sentinel", Advisory: true}

	findings, err := toolFindings([]checktool.Finding{found}, nil)
	want := finding{Code: found.Code, Message: found.Message, Location: found.Location, Source: found.Source, URL: found.URL, Advisory: true}
	if err != nil || len(findings) != 1 || findings[0] != want {
		t.Fatalf("toolFindings = %+v, %v; want %+v", findings, err, want)
	}

	if findings, err := toolFindings([]checktool.Finding{found}, errors.New("tool error")); err == nil || findings != nil {
		t.Fatalf("an error must carry no findings: %+v, %v", findings, err)
	}
	if findings, err := toolFindings(nil, nil); err != nil || findings != nil {
		t.Fatalf("a pass has no findings: %+v, %v", findings, err)
	}
}

func TestFindingsDetailsMatchTheDaggerEnvelope(t *testing.T) {
	t.Parallel()
	details := findingsDetails([]finding{{Code: "SA5001", Message: "m", Location: location{File: "a.go", Line: 2, Column: 3}}})

	for _, want := range []string{`"findings"`, `"code":"SA5001"`, `"file":"a.go"`, `"line":2`} {
		if !strings.Contains(string(details), want) {
			t.Fatalf("details missing %s: %s", want, details)
		}
	}
}
