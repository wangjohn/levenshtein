package verify

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestHintFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    finding
		want string
	}{
		{"formatting names the file", lintFinding("pkg/a.go", 1, "LV1005", "not formatted"), "run gofmt -w pkg/a.go; see " + ruleDocs["LV1005"]},
		{"modernize names its rule", lintFinding("a.go", 1, "minmax", "use max"), "run go fix -minmax ./... in the module"},
		{"errcheck", lintFinding("a.go", 1, "errcheck", "unchecked"), "handle the error, or discard it explicitly with _ = and a comment giving the reason"},
		{"untidy module", finding{Code: "go-mod", Message: "diff go.mod\n+require x v1", Location: location{File: "services/api", Line: 1}}, "run go mod tidy in services/api"},
		{"modified download", finding{Code: "go-mod", Message: "example.com/x v1.0.0: dir has been modified (/m)", Location: location{File: ".", Line: 1}}, ""},
		{"go.sum mismatch", finding{Code: "go-mod", Message: "verifying x: checksum mismatch\nSECURITY ERROR", Location: location{File: ".", Line: 1}}, ""},
		{"no mechanical fix", lintFinding("a.go", 1, "SA5001", "check first"), ""},
	} {
		if got := hintFor(tc.f); got != tc.want {
			t.Errorf("%s: hint = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWithHintsKeepsExistingHints(t *testing.T) {
	check := lintCheck("lint", ".")
	given := finding{Code: "errcheck", Message: "m", Location: location{File: "a.go", Line: 1}, Hint: "already said"}
	report := WithHints(reportOf([]PlannedCheck{check}, failedResult("lint", given, lintFinding("b.go", 1, "LV1005", "m"))))

	findings := detailFindings(report.Results[0].Details)
	if findings[0].Hint != "already said" || !strings.HasPrefix(findings[1].Hint, "run gofmt -w b.go") || report.Results[0].Status != StatusFailed {
		t.Fatalf("hints: %+v", findings)
	}
}

// Every rule link must land on a heading docs/checks.md has, using GitHub's
// anchor for it: lowercase, punctuation dropped, spaces as hyphens.
func TestRuleDocsAnchorsExist(t *testing.T) {
	data, err := os.ReadFile("../../docs/checks.md")
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]bool{}
	drop := regexp.MustCompile(`[^a-z0-9 -]`)
	for line := range strings.SplitSeq(string(data), "\n") {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			anchors[strings.ReplaceAll(drop.ReplaceAllString(strings.ToLower(heading), ""), " ", "-")] = true
		}
	}

	for code, link := range ruleDocs {
		anchor, ok := strings.CutPrefix(link, checksDoc+"#")
		if !ok || !anchors[anchor] {
			t.Errorf("%s links to %q, which docs/checks.md has no heading for", code, link)
		}
		if !strings.Contains(hints[code], link) {
			t.Errorf("%s's hint does not link its rule: %q", code, hints[code])
		}
	}
}
