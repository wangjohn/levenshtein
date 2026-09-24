package verify

import (
	"strings"
)

// checksDoc is where every Levenshtein rule is explained. Hints link to it by
// section, so a reader outside this repository can follow them.
const checksDoc = "https://github.com/wangjohn/levenshtein/blob/main/docs/checks.md"

// ruleDocs links each Levenshtein rule to its section of docs/checks.md.
var ruleDocs = map[string]string{
	"LV1001": checksDoc + "#typed-choices-lv1001",
	"LV1002": checksDoc + "#construct-value-records-together-lv1002",
	"LV1003": checksDoc + "#one-field-per-line-lv1003",
	"LV1004": checksDoc + "#a-blank-line-between-declarations-lv1004",
	"LV1005": checksDoc + "#formatted-files-lv1005",
	"LV1006": checksDoc + "#tests-that-can-fail-lv1006",
}

// hints are one-line instructions for codes whose fix is mechanical and well
// known. {file} stands for the finding's repository-relative file, which for
// a module-level finding such as go-mod's is the module directory. A hint only
// says what to do; Levenshtein never applies it. Keep the table small: a code
// earns an entry only when the fix does not depend on the code around it.
var hints = map[string]string{
	"LV1001":           "give the value a defined string type with typed constants; see " + ruleDocs["LV1001"],
	"LV1002":           "build the struct in one composite literal after computing its fields; see " + ruleDocs["LV1002"],
	"LV1003":           "declare each struct field on its own line; see " + ruleDocs["LV1003"],
	"LV1004":           "add a blank line above the declaration and its doc comment; see " + ruleDocs["LV1004"],
	"LV1005":           "run gofmt -w {file}; see " + ruleDocs["LV1005"],
	"LV1006":           "give the test an assertion that can fail, or make its skip conditional; see " + ruleDocs["LV1006"],
	"minmax":           "run go fix -minmax ./... in the module",
	"mapsloop":         "run go fix -mapsloop ./... in the module",
	"slicescontains":   "run go fix -slicescontains ./... in the module",
	"stringscutprefix": "run go fix -stringscutprefix ./... in the module",
	"stringsseq":       "run go fix -stringsseq ./... in the module",
	"errcheck":         "handle the error, or discard it explicitly with _ = and a comment giving the reason",
	"go-mod":           "run go mod tidy in {file}",
	"go-generate":      "run go generate ./... in the module and commit what it writes",
	baselineStaleCode:  "delete the entry or lower its count, or rewrite the file with verify <run> --write-baseline",
}

// hintFor returns the hint for one finding, or "" when its fix is not
// mechanical. go-mod reports tidy's diff and go mod verify's mismatches under
// one code, and only the first is fixed by tidying.
func hintFor(f finding) string {
	template, ok := hints[f.Code]
	if !ok {
		return ""
	}
	if f.Code == string(CheckGoMod) && (strings.Contains(f.Message, "SECURITY ERROR") || modifiedModule.MatchString(f.Message)) {
		return ""
	}
	return strings.ReplaceAll(template, "{file}", f.Location.File)
}

// WithHints fills in the hint of every finding that has a mechanical fix and
// does not already carry one. It changes only the findings in each result's
// details, never a status, so it can run on a fresh or a saved report alike.
func WithHints(report Report) Report {
	return report.mapFindings(func(_ PlannedCheck, findings []finding) []finding {
		hinted := make([]finding, 0, len(findings))
		for _, f := range findings {
			if f.Hint == "" {
				f.Hint = hintFor(f)
			}
			hinted = append(hinted, f)
		}
		return hinted
	})
}
