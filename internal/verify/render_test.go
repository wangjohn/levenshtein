package verify

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// renderFixture is one run of every shape a renderer meets: located findings
// out of order, one of them baselined, a module-level tool finding, a tool
// error, and a cached pass.
func renderFixture() Report {
	lint := lintCheck("lint/api", "services/api")
	vet := lintCheck("vet", ".")
	vet.Check.Kind = CheckGoVet
	test := lintCheck("test", ".")
	test.Check.Kind = CheckGoTest
	mod := lintCheck("mod", ".")
	mod.Check.Kind = CheckGoMod

	lintResult := failedResult("lint/api",
		lintFinding("services/api/b.go", 3, "errcheck", "unchecked error"),
		lintFinding("services/api/a.go", 10, "LV1005", "file is not gofmt-formatted"),
		finding{Code: "SA5001", Message: "old debt", Location: location{File: "services/api/c.go", Line: 1}, Baselined: true},
	)
	vetResult := failedResult("vet", finding{Code: "go-vet", Message: "# example.com/app\nx.go:1:2: bad, really: 100%", Location: location{File: ".", Line: 1}})
	testResult := Result{ID: "test", Status: StatusError, Error: "go test could not build x\nfirst cause"}
	modResult := Result{ID: "mod", Status: StatusPassed, Cache: CacheInfo{Status: CacheHit}}

	report := reportOf([]PlannedCheck{lint, vet, test, mod}, lintResult, vetResult, testResult, modResult)
	report.Baseline = &BaselineSummary{File: testBaselinePath, Baselined: 1}
	return WithHints(report)
}

func render(t *testing.T, report Report, format Format, options RenderOptions) string {
	t.Helper()
	var out bytes.Buffer
	if err := Render(&out, report, format, options); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestRenderText(t *testing.T) {
	got := render(t, renderFixture(), FormatText, RenderOptions{})

	want := `services/api/a.go:10:2: LV1005 file is not gofmt-formatted
    hint: run gofmt -w services/api/a.go; see https://github.com/wangjohn/levenshtein/blob/main/docs/checks.md#formatted-files-lv1005
services/api/b.go:3:2: errcheck unchecked error
    hint: handle the error, or discard it explicitly with _ = and a comment giving the reason
.: go-vet
    # example.com/app
    x.go:1:2: bad, really: 100%

lint/api  failed  2 findings; 1 baselined
vet       failed  1 finding
test      error   go test could not build x
mod       passed  cached

test:
    go test could not build x
    first cause

branch: failed, 1 of 4 checks passed, 1 baselined finding not shown
`
	if got != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTextPrefixesPaths(t *testing.T) {
	got := render(t, renderFixture(), FormatText, RenderOptions{PathPrefix: "app"})
	if !strings.HasPrefix(got, "app/services/api/a.go:10:2: LV1005") || !strings.Contains(got, "\napp: go-vet\n") {
		t.Fatalf("paths not prefixed:\n%s", got)
	}
}

func TestRenderGitHub(t *testing.T) {
	got := render(t, renderFixture(), FormatGitHub, RenderOptions{PathPrefix: "app"})

	want := strings.Join([]string{
		"::error file=app/services/api/a.go,line=10,col=2,title=LV1005 (lint/api)::file is not gofmt-formatted%0A%0Ahint: run gofmt -w services/api/a.go; see https://github.com/wangjohn/levenshtein/blob/main/docs/checks.md#formatted-files-lv1005",
		"::error file=app/services/api/b.go,line=3,col=2,title=errcheck (lint/api)::unchecked error%0A%0Ahint: handle the error, or discard it explicitly with _ = and a comment giving the reason",
		"::error title=go-vet (vet)::# example.com/app%0Ax.go:1:2: bad, really: 100%25",
		"::error title=Levenshtein test error::go test could not build x%0Afirst cause",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("github:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderGitHubAnnotatesFailuresWithoutFindings(t *testing.T) {
	check := lintCheck("script", ".")
	check.Check.Kind = CheckCommand
	report := reportOf([]PlannedCheck{check}, Result{ID: "script", Status: StatusFailed, Error: "exit status 1"})

	if got := render(t, report, FormatGitHub, RenderOptions{}); got != "::error title=Levenshtein script failed::exit status 1\n" {
		t.Fatalf("github: %q", got)
	}
}

func TestGitHubEscaping(t *testing.T) {
	if got := escapeData("100% done\r\nnext ::error::"); got != "100%25 done%0D%0Anext ::error::" {
		t.Errorf("data: %q", got)
	}
	if got := escapeProperty("a,b:c%\nd"); got != "a%2Cb%3Ac%25%0Ad" {
		t.Errorf("property: %q", got)
	}
}

func TestRenderSARIF(t *testing.T) {
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID      string `json:"id"`
						HelpURI string `json:"helpUri"`
						Help    *struct {
							Text string `json:"text"`
						} `json:"help"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Invocations []struct {
				ExecutionSuccessful bool `json:"executionSuccessful"`
				Notifications       []struct {
					Level   string `json:"level"`
					Message struct {
						Text string `json:"text"`
					} `json:"message"`
				} `json:"toolExecutionNotifications"`
			} `json:"invocations"`
			Results []struct {
				RuleID        string `json:"ruleId"`
				RuleIndex     int    `json:"ruleIndex"`
				Level         string `json:"level"`
				BaselineState string `json:"baselineState"`
				Locations     []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI       string `json:"uri"`
							URIBaseID string `json:"uriBaseId"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine   int `json:"startLine"`
							StartColumn int `json:"startColumn"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
				Properties struct {
					Check string    `json:"check"`
					Kind  CheckKind `json:"kind"`
				} `json:"properties"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(render(t, renderFixture(), FormatSARIF, RenderOptions{PathPrefix: "my app"})), &log); err != nil {
		t.Fatal(err)
	}

	if log.Version != "2.1.0" || len(log.Runs) != 1 || log.Runs[0].Tool.Driver.Name != "Levenshtein" {
		t.Fatalf("log: %+v", log)
	}
	run := log.Runs[0]
	var ids []string
	for _, rule := range run.Tool.Driver.Rules {
		ids = append(ids, rule.ID)
	}
	if strings.Join(ids, ",") != "LV1005,SA5001,errcheck" {
		t.Fatalf("rules: %v", ids)
	}
	if run.Tool.Driver.Rules[0].HelpURI != ruleDocs["LV1005"] || run.Tool.Driver.Rules[1].HelpURI != "https://staticcheck.dev/docs/checks/#SA5001" || run.Tool.Driver.Rules[2].HelpURI != "" {
		t.Fatalf("help links: %+v", run.Tool.Driver.Rules)
	}
	if help := run.Tool.Driver.Rules[0].Help; help == nil || !strings.HasPrefix(help.Text, "run gofmt -w <file>") {
		t.Fatalf("rule help: %+v", help)
	}

	if len(run.Results) != 3 {
		t.Fatalf("results: %+v", run.Results)
	}
	first := run.Results[0]
	location := first.Locations[0].PhysicalLocation
	if first.RuleID != "LV1005" || first.RuleIndex != 0 || first.Level != "error" || first.BaselineState != "new" || first.Properties.Check != "lint/api" || first.Properties.Kind != CheckGoLint {
		t.Fatalf("first result: %+v", first)
	}
	if location.ArtifactLocation.URI != "my%20app/services/api/a.go" || location.ArtifactLocation.URIBaseID != "%SRCROOT%" || location.Region.StartLine != 10 || location.Region.StartColumn != 2 {
		t.Fatalf("first location: %+v", location)
	}
	if baselined := run.Results[2]; baselined.RuleID != "SA5001" || baselined.RuleIndex != 1 || baselined.Level != "warning" || baselined.BaselineState != "unchanged" {
		t.Fatalf("baselined result: %+v", baselined)
	}

	invocation := run.Invocations[0]
	if invocation.ExecutionSuccessful || len(invocation.Notifications) != 2 {
		t.Fatalf("invocation: %+v", invocation)
	}
	if !strings.HasPrefix(invocation.Notifications[0].Message.Text, "vet go-vet: # example.com/app") || !strings.HasPrefix(invocation.Notifications[1].Message.Text, "test error: go test could not build x") {
		t.Fatalf("notifications: %+v", invocation.Notifications)
	}
}

func TestRenderSARIFWithoutBaselineOmitsBaselineState(t *testing.T) {
	report := renderFixture()
	report.Baseline = nil
	if got := render(t, report, FormatSARIF, RenderOptions{}); strings.Contains(got, `"baselineState": "new"`) {
		t.Fatalf("baselineState without a baseline:\n%s", got)
	}
}

// SARIF requires results to be an array whenever a log represents a scan, and
// code scanning rejects an upload whose results are null, so a clean run must
// still write an empty array.
func TestRenderSARIFCleanRunHasEmptyResults(t *testing.T) {
	report := reportOf([]PlannedCheck{lintCheck("lint", ".")}, Result{ID: "lint", Status: StatusPassed})

	var log struct {
		Runs []struct {
			Results json.RawMessage `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(render(t, report, FormatSARIF, RenderOptions{})), &log); err != nil {
		t.Fatal(err)
	}

	if len(log.Runs) != 1 || string(log.Runs[0].Results) != "[]" {
		t.Fatalf("results of a clean run: %s", log.Runs[0].Results)
	}
}

func TestRenderJSONRoundTrips(t *testing.T) {
	report := renderFixture()
	var decoded Report
	if err := json.Unmarshal([]byte(render(t, report, FormatJSON, RenderOptions{})), &decoded); err != nil {
		t.Fatal(err)
	}
	if render(t, decoded, FormatText, RenderOptions{}) != render(t, report, FormatText, RenderOptions{}) {
		t.Fatal("a saved JSON report must render as the report it came from")
	}
	if findings := detailFindings(decoded.Results[0].Details); findings[0].Hint == "" || !findings[2].Baselined {
		t.Fatalf("JSON dropped hints or baseline marks: %+v", findings)
	}
}

func TestRenderSkipsSemanticFindings(t *testing.T) {
	semantic := lintCheck("review", ".")
	semantic.Check.Kind = CheckSemanticLint
	details := json.RawMessage(`{"findings":[{"question":"q","severity":"advice","path":"a.go","line":3,"message":"consider this"}]}`)
	report := reportOf([]PlannedCheck{semantic}, Result{ID: "review", Status: StatusPassed, Details: details})

	if got := render(t, report, FormatGitHub, RenderOptions{}); got != "" {
		t.Fatalf("advisory findings must not become annotations: %q", got)
	}
	if got := render(t, WithHints(report), FormatText, RenderOptions{}); strings.Contains(got, "consider this") {
		t.Fatalf("advisory findings must not be listed:\n%s", got)
	}
}

func TestRenderRejectsUnknownFormat(t *testing.T) {
	//lint:ignore LV1001 the test needs a value outside the declared formats.
	if err := Render(&bytes.Buffer{}, Report{}, Format("xml"), RenderOptions{}); err == nil {
		t.Fatal("unknown format accepted")
	}
}
