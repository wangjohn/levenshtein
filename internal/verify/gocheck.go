package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// helperGocheck is the command both executors run for the shared checks that
// judge a whole module, so each verdict is decided in one place:
// runner/lint/gocheck.
var helperGocheck = helper{Name: "levenshtein-gocheck", Module: "runner/lint", Pkg: "./cmd/levenshtein-gocheck"}

// gocheckRun runs levenshtein-gocheck for one check and turns its report into
// findings, with its notes as the output a person reads.
func gocheckRun(ctx context.Context, kind CheckKind, dir string, args, env []string) ([]finding, toolRun, error) {
	run, err := runTool(ctx, dir, args, env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	found, notes, err := checktool.GocheckReport(checktool.Kind(kind), run.diagnostics())
	if err != nil {
		return nil, run, err
	}
	findings, err := toolFindings(found, nil)
	run.Stdout = strings.Join(notes, "\n")
	return findings, run, err
}

// goImports checks the target's packages against its layering rules. It
// reads the imports go list reports and the files' own import declarations,
// so it needs neither module downloads nor type checking.
func (n *Native) goImports(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	rules, err := importRules(req.Check)
	if err != nil {
		return nil, toolRun{}, err
	}
	if _, err := os.Stat(filepath.Join(work.Dir, "go.mod")); err != nil {
		return nil, toolRun{}, fmt.Errorf("module %q needs a readable go.mod: %w", req.Target.Dir, err)
	}
	binary, err := build(ctx, req, work, helperGocheck)
	if err != nil {
		return nil, toolRun{}, err
	}

	args := []string{binary, "imports", "-rules=" + rules, "-prefix=" + filepath.ToSlash(req.Target.Dir)}
	return gocheckRun(ctx, CheckGoImports, work.Dir, args, work.Env)
}

// importRules is the rules argument both executors pass to the helper.
func importRules(check Check) (string, error) {
	if check.Imports == nil {
		return "", fmt.Errorf(`go-imports needs an "imports" object with its rules`)
	}
	data, err := json.Marshal(check.Imports)
	return string(data), err
}

// validateImportRules rejects go-imports rules the check could not apply as
// written, when the run is planned. It is a copy of ValidateImportRules in
// runner/lint/gocheck, which checks the same rules again when it runs; both
// tests load runner/testdata/import-rules.json. Whether a package pattern
// matches a package is only known when the check runs.
func validateImportRules(config ImportsCheck) error {
	if len(config.Rules) == 0 {
		return fmt.Errorf("go-imports needs a nonempty rules list")
	}
	for i, rule := range config.Rules {
		number := i + 1
		if len(rule.Packages) == 0 {
			return fmt.Errorf("go-imports rule %d needs a nonempty packages list", number)
		}
		for _, entry := range rule.Packages {
			if !localImportPattern(entry) {
				return fmt.Errorf("go-imports rule %d: package pattern %q must be \".\" or start with \"./\", like \"./internal/store/...\"", number, entry)
			}
		}
		if len(rule.Deny) == 0 && len(rule.Allow) == 0 {
			return fmt.Errorf("go-imports rule %d needs a deny or an allow list", number)
		}
		for _, entry := range slices.Concat(rule.Deny, rule.Allow) {
			if entry != "std" && !localImportPattern(entry) && !importPathPattern(entry) {
				return fmt.Errorf("go-imports rule %d: import pattern %q must be an import path pattern such as \"example.com/app/internal/...\", a \"./\" pattern, or \"std\"", number, entry)
			}
		}
		if rule.Tests != "" && rule.Tests != ImportTestsInclude && rule.Tests != ImportTestsExclude {
			return fmt.Errorf("go-imports rule %d: tests %q must be %q or %q", number, rule.Tests, ImportTestsInclude, ImportTestsExclude)
		}
		if strings.TrimSpace(rule.Reason) == "" {
			return fmt.Errorf("go-imports rule %d needs a reason, which every finding it reports repeats", number)
		}
	}
	return nil
}

// localImportPattern is "." or a pattern relative to the target directory.
func localImportPattern(entry string) bool {
	rest, ok := strings.CutPrefix(entry, "./")
	return entry == "." || (ok && importPathPattern(rest))
}

// importPathPattern is a slash-separated path with an optional "..."
// wildcard: no empty, "." or ".." element, and nothing an import path cannot
// hold.
func importPathPattern(entry string) bool {
	if entry == "" || strings.ContainsAny(entry, " \t\r\n\"'`\\;:,\x00") {
		return false
	}
	for element := range strings.SplitSeq(entry, "/") {
		//lint:ignore LV1001 path elements are arbitrary text; these are the two relative spellings Go rejects.
		if element == "" || element == "." || element == ".." {
			return false
		}
	}
	return true
}
