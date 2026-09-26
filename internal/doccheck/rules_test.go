package doccheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// backticked is one code span in a table cell, such as `errcheck` or `SA*`.
var backticked = regexp.MustCompile("`([^`]+)`")

// TestRuleTablesFollowTheShippedSelection keeps docs/rules.md's two rule
// tables in step with the selection runner/toolchain.json gives every go-lint
// check: every rule the on-by-default table lists must be on, and every rule
// the opt-in table lists must be off and absent from the other table. It
// judges a name the way the linter does, with checktool.Allowed, so a family
// row such as `SA*` counts as on unless the selection turns the family off.
func TestRuleTablesFollowTheShippedSelection(t *testing.T) {
	selection := shippedSelection(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	onByDefault := ruleTable(t, string(data), "Rules on by default")
	optIn := ruleTable(t, string(data), "Opt-in rules")
	if len(onByDefault) < 50 {
		t.Fatalf("found only %d rules on by default; did the table's layout change?", len(onByDefault))
	}

	for _, problem := range ruleTableProblems(selection, onByDefault, optIn) {
		t.Error(problem)
	}
}

// ruleTableProblems says what is wrong with the two tables under a selection.
func ruleTableProblems(selection, onByDefault, optIn []string) []string {
	var problems []string
	for _, rule := range onByDefault {
		if !checktool.Allowed(selection, rule) {
			problems = append(problems, "docs/rules.md lists "+rule+" as on by default, but runner/toolchain.json turns it off; move it to the opt-in rules table")
		}
	}
	for _, rule := range optIn {
		if checktool.Allowed(selection, rule) {
			problems = append(problems, "docs/rules.md lists "+rule+" as opt-in, but runner/toolchain.json turns it on; move it to the rules on by default")
		}
		if slices.Contains(onByDefault, rule) {
			problems = append(problems, "docs/rules.md lists opt-in rule "+rule+" in the rules on by default too")
		}
	}
	return problems
}

func TestRuleTableProblemsCatchMisplacedRules(t *testing.T) {
	selection := []string{"all", "-ST1000", "-gocognit"}

	if problems := ruleTableProblems(selection, []string{"SA*", "errcheck"}, []string{"gocognit"}); len(problems) != 0 {
		t.Fatalf("tables that follow the selection: %v", problems)
	}
	if problems := ruleTableProblems(selection, []string{"errcheck", "gocognit"}, []string{"gocognit"}); len(problems) != 2 {
		t.Fatalf("an opt-in rule in the on-by-default table must be reported twice: %v", problems)
	}
	if problems := ruleTableProblems(selection, []string{"errcheck"}, []string{"errcheck"}); len(problems) != 2 {
		t.Fatalf("a rule that is on must not be listed as opt-in: %v", problems)
	}
}

// shippedSelection is the checks list runner/toolchain.json ships.
func shippedSelection(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "runner", "toolchain.json"))
	if err != nil {
		t.Fatal(err)
	}
	var toolchain struct {
		Checks []string `json:"checks"`
	}
	if err := json.Unmarshal(data, &toolchain); err != nil {
		t.Fatal(err)
	}
	if len(toolchain.Checks) == 0 {
		t.Fatal("runner/toolchain.json has no checks list")
	}
	return toolchain.Checks
}

// ruleTable is every rule name in the first column of the table under a
// level-two heading: each code span in the cell, or the cell itself when it
// has none, such as LV1001.
func ruleTable(t *testing.T, text, heading string) []string {
	t.Helper()
	_, section, found := strings.Cut(text, "\n## "+heading+"\n")
	if !found {
		t.Fatalf("docs/rules.md has no %q section", heading)
	}
	section, _, _ = strings.Cut(section, "\n## ")

	var rules []string
	for line := range strings.SplitSeq(section, "\n") {
		cells := strings.Split(line, "|")
		if !strings.HasPrefix(line, "|") || len(cells) < 3 {
			continue
		}
		cell := strings.TrimSpace(cells[1])
		if cell == "Rule" || strings.Trim(cell, "- ") == "" {
			continue
		}
		spans := backticked.FindAllStringSubmatch(cell, -1)
		if len(spans) == 0 {
			rules = append(rules, cell)
		}
		for _, span := range spans {
			rules = append(rules, span[1])
		}
	}
	if len(rules) == 0 {
		t.Fatalf("docs/rules.md's %q section has no rule table", heading)
	}
	return rules
}
