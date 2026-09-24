package community

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// patternCases is runner/testdata/community-patterns.json, which the CLI's
// copy of the pattern syntax and selection loads too.
type patternCases struct {
	Syntax []struct {
		Pattern string `json:"pattern"`
		Valid   bool   `json:"valid"`
	} `json:"syntax"`
	Selection []struct {
		Name     string   `json:"name"`
		Patterns []string `json:"patterns"`
		Code     string   `json:"code"`
		Want     bool     `json:"want"`
	} `json:"selection"`
}

func loadPatternCases(t *testing.T) patternCases {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "community-patterns.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases patternCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestPatternSyntaxMatchesTheSharedTable(t *testing.T) {
	for _, test := range loadPatternCases(t).Syntax {
		if got := Pattern.MatchString(test.Pattern); got != test.Valid {
			t.Errorf("Pattern.MatchString(%q) = %v, want %v", test.Pattern, got, test.Valid)
		}
	}
}

// The resolver applies the table's patterns to a module that has the table's
// code, the way a check's selection reaches it.
func TestSelectionMatchesTheSharedTable(t *testing.T) {
	for _, test := range loadPatternCases(t).Selection {
		t.Run(test.Name, func(t *testing.T) {
			modules := []Module{
				{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Analyzers: []*analysis.Analyzer{testAnalyzer("nopanic", "d"), testAnalyzer("sentinel", "d")}},
				{Path: "example.com/sql", Version: "v1.0.0", Namespace: "sql", Analyzers: []*analysis.Analyzer{testAnalyzer("rows", "d")}},
			}
			r, err := newResolver(modules, Config{Modules: []ModuleConfig{
				{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: []string{"errs_*"}},
				{Path: "example.com/sql", Version: "v1.0.0", Namespace: "sql", Select: []string{"sql_*"}},
			}})
			if err != nil {
				t.Fatal(err)
			}

			state, err := r.apply(test.Patterns, map[string]bool{})
			if err != nil {
				t.Fatal(err)
			}

			if got := state[test.Code]; got != test.Want {
				t.Errorf("selected %s = %v, want %v", test.Code, got, test.Want)
			}
		})
	}
}
