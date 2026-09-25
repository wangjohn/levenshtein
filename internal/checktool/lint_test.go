package checktool

import "testing"

func TestLintExitStatusAndDiagnosticsAgree(t *testing.T) {
	valid := `{"code":"SA5001","message":"defer before error check","location":{"file":"/src/close.go","line":7,"column":2}}`

	findings, err := LintFindings(Run{ExitCode: 1, Stdout: valid}, []string{"SA5001"}, "/src")
	want := Finding{Code: "SA5001", Message: "defer before error check", Location: Location{File: "close.go", Line: 7, Column: 2}, URL: "https://staticcheck.dev/docs/checks/#SA5001"}
	if err != nil || len(findings) != 1 || findings[0] != want {
		t.Fatalf("lost lint diagnostic: %+v, %v", findings, err)
	}

	for _, tc := range []Run{
		{ExitCode: 0, Stdout: valid}, {ExitCode: 1}, {ExitCode: 2, Stderr: "crash"},
		{ExitCode: 1, Stdout: "not JSON"}, {ExitCode: 0, Stderr: "warning: no packages"},
		{ExitCode: 1, Stdout: `{"code":"compile","message":"syntax error"}`},
		{ExitCode: 1, Stdout: valid, Stderr: "panic"},
	} {
		if _, err := LintFindings(tc, []string{"SA5001"}, "/src"); err == nil {
			t.Errorf("accepted invalid tool result: %+v", tc)
		}
	}
	if _, err := LintFindings(Run{}, []string{"SA5001"}, "/src"); err != nil {
		t.Fatal(err)
	}
}

func TestStaticcheckWildcardDoesNotAcceptCompilerErrors(t *testing.T) {
	for _, code := range []string{"SA4006", "compile"} {
		output := `{"code":"` + code + `","message":"finding","location":{"file":"/src/a.go","line":1}}`
		if _, err := LintFindings(Run{ExitCode: 1, Stdout: output}, []string{"SA*"}, "/src"); (err == nil) != (code == "SA4006") {
			t.Fatalf("unexpected result for %s: %v", code, err)
		}
	}
}

// selectionCase is one row of runner/testdata/selection.json.
type selectionCase struct {
	Name   string   `json:"name"`
	Checks []string `json:"checks"`
	Code   string   `json:"code"`
	Want   bool     `json:"want"`
}

// The core linter keeps its own copy of this filter, and both load one table.
func TestCheckSelectionMatchesTheLinter(t *testing.T) {
	table := sharedTable[struct {
		Cases []selectionCase `json:"cases"`
	}](t, "selection.json")
	if len(table.Cases) == 0 {
		t.Fatal("runner/testdata/selection.json has no cases")
	}

	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := Allowed(tc.Checks, tc.Code); got != tc.Want {
				t.Fatalf("Allowed(%v, %q) = %v, want %v", tc.Checks, tc.Code, got, tc.Want)
			}
		})
	}
}

// registeredCase is one row of runner/testdata/registered.json.
type registeredCase struct {
	Name    string   `json:"name"`
	Checks  []string `json:"checks"`
	Matches bool     `json:"matches"`
}

// A pattern a go-lint check adds must match a rule the linter lists.
func TestAddedChecksMustMatchARegisteredRule(t *testing.T) {
	table := sharedTable[struct {
		Listing string           `json:"listing"`
		Cases   []registeredCase `json:"cases"`
	}](t, "registered.json")
	if len(table.Cases) == 0 {
		t.Fatal("runner/testdata/registered.json has no cases")
	}

	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if err := Registered(tc.Checks, table.Listing); (err == nil) != tc.Matches {
				t.Fatalf("Registered(%v) = %v, want matches=%v", tc.Checks, err, tc.Matches)
			}
		})
	}
}

func TestCoreURLsMatchTheSharedTable(t *testing.T) {
	table := sharedTable[struct {
		Cases []struct {
			Code string `json:"code"`
			URL  string `json:"url"`
		} `json:"cases"`
	}](t, "core-urls.json")
	if len(table.Cases) == 0 {
		t.Fatal("runner/testdata/core-urls.json has no cases")
	}

	for _, test := range table.Cases {
		if got := CoreURL(test.Code); got != test.URL {
			t.Errorf("CoreURL(%q) = %q, want %q", test.Code, got, test.URL)
		}
	}
}
