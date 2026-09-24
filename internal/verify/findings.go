package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// finding and location mirror the diagnostic shape the Dagger module attaches
// as its levenshteinFindings extension, so a report says the same thing however
// the check was executed. The runner is a separate Go module and cannot be
// imported, so these are a deliberate copy; change both together.
type finding struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location location `json:"location"`
	// Source names the rule module a community finding came from, as
	// path@version; core findings leave it out.
	Source string `json:"source,omitempty"`
	// URL documents the rule that reported the finding, when it has a page.
	URL string `json:"url,omitempty"`
	// Advisory findings are reported without failing the check.
	Advisory bool `json:"advisory"`
}

// staticcheckCode is a code from one of Staticcheck's own families.
var staticcheckCode = regexp.MustCompile(`^(SA|S|ST|QF)[0-9]{4}$|^U1000$`)

// coreURL is the page that documents a core lint rule: Staticcheck's own page
// for its families, and otherwise this repository's rule list, which gives the
// reason for every rule. Every copy loads runner/testdata/core-urls.json in its
// tests.
func coreURL(code string) string {
	if staticcheckCode.MatchString(code) {
		return "https://staticcheck.dev/docs/checks/#" + code
	}
	return "https://github.com/wangjohn/levenshtein/blob/main/docs/checks.md#go-lint-rules"
}

type location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// allowed and selects are copies of the runner's (runner/main.go), which
// reproduce Staticcheck's filterAnalyzerNames (lintcmd/lint.go in
// honnef.co/go/tools v0.8.1) for one code; the core linter keeps a third
// (runner/lint/cmd/levenshtein-lint). Every copy loads the same table,
// runner/testdata/selection.json, in their tests, so a change to one that is
// not made to the other fails a test instead of relying on memory.
//
// Patterns apply in order and the last one that matches wins, so
// "all,-SA5001" turns SA5001 off while "-SA5001,all" turns it back on. A "-"
// prefix turns a code off rather than on. Anything not selected, including a
// compile error reported as a diagnostic, is not a lint finding and makes the
// run an error rather than a failure.
func allowed(checks []string, code string) bool {
	selected := false
	for _, check := range checks {
		pattern := check
		enable := true
		if len(pattern) > 1 && pattern[0] == '-' {
			pattern = pattern[1:]
			enable = false
		}
		if selects(pattern, code) {
			selected = enable
		}
	}
	return selected
}

// selects matches one pattern the way Staticcheck does, ignoring case: "all"
// or "*" matches every code, a trailing "*" after letters matches that exact
// category (S* matches S1002 but not SA5001), a trailing "*" after a digit is a
// plain prefix (SA5* matches SA5001), and anything else is a literal name.
func selects(pattern, code string) bool {
	pattern = strings.ToLower(pattern)
	code = strings.ToLower(code)

	//lint:ignore LV1001 patterns are free-form user input; these are two spellings of one wildcard, not an enum.
	if pattern == "*" || pattern == "all" {
		return true
	}
	prefix, glob := strings.CutSuffix(pattern, "*")
	if !glob {
		return pattern == code
	}
	if strings.IndexFunc(prefix, unicode.IsNumber) != -1 {
		return strings.HasPrefix(code, prefix)
	}
	category := code
	if digit := strings.IndexFunc(code, unicode.IsNumber); digit != -1 {
		category = code[:digit]
	}
	return category == prefix
}

// parseFindings turns one linter run into diagnostics, refusing any result
// that does not agree with itself: an unexpected exit code, output that is not
// the configured checks, an exit status that contradicts the diagnostics, or
// anything at all on stderr.
func parseFindings(exitCode int, stdout, stderr string, checks []string, root string) ([]finding, error) {
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("the linter exited %d: %s\n%s", exitCode, stderr, stdout)
	}

	var findings []finding
	decoder := json.NewDecoder(strings.NewReader(stdout))
	for {
		var diagnostic finding
		err := decoder.Decode(&diagnostic)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid linter JSON: %w", err)
		}
		if !allowed(checks, diagnostic.Code) || diagnostic.Message == "" || diagnostic.Location.File == "" || diagnostic.Location.Line < 1 {
			return nil, fmt.Errorf("unexpected diagnostic (possibly a compile error): %s", stdout)
		}
		diagnostic.Location.File = repositoryPath(root, diagnostic.Location.File)
		diagnostic.URL = coreURL(diagnostic.Code)
		findings = append(findings, diagnostic)
	}

	if (exitCode == 0 && len(findings) != 0) || (exitCode == 1 && len(findings) == 0) {
		return nil, fmt.Errorf("linter exit %d does not match diagnostics: %s\n%s", exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		return nil, fmt.Errorf("the linter could not produce a clean result: %s", stderr)
	}
	return findings, nil
}

// repositoryPath reports a tool's file location the way the Dagger path does:
// relative to the source root, so the same diagnostic reads the same whichever
// executor produced it. A path outside the root is left alone.
func repositoryPath(root, file string) string {
	if root == "" || !filepath.IsAbs(file) {
		return file
	}

	rel, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(rel) {
		return file
	}
	return rel
}

// commandFindings preserves a standalone tool's own output, including its
// precise locations, as one finding. Only the tool's documented failure code
// means diagnostics; every other nonzero exit is a tool error.
func commandFindings(check CheckKind, module string, exitCode int, stdout, stderr string) ([]finding, error) {
	if exitCode == 0 {
		return nil, nil
	}

	message := strings.TrimSpace(stdout + "\n" + stderr)
	failureCode := 1
	if check == CheckGoVuln {
		failureCode = 3 // govulncheck distinguishes vulnerabilities from tool errors.
	}
	if exitCode != failureCode || message == "" {
		return nil, fmt.Errorf("%s exited %d: %s", check, exitCode, message)
	}
	return []finding{{
		Code:     string(check),
		Message:  message,
		Location: location{File: module, Line: 1},
	}}, nil
}

// modStep is one of the two go commands a go-mod check runs, in order: whether
// go.mod and go.sum are already what tidy would write, then whether the
// downloaded dependencies still match the hashes go.sum recorded.
type modStep string

const (
	modTidy   modStep = "tidy -diff"
	modVerify modStep = "verify"
)

var modSteps = []modStep{modTidy, modVerify}

func (s modStep) args() []string {
	return append([]string{"go", "mod"}, strings.Fields(string(s))...)
}

// modifiedModule is how go mod verify names a download that no longer matches
// the hash recorded when it was fetched.
var modifiedModule = regexp.MustCompile(`(?m)^\S+ \S+: (zip has been modified|dir has been modified|missing ziphash)`)

// modFindings tells a go-mod step's diagnostics from a tool error. Both
// commands exit 1 either way, so the output decides: tidy -diff prints a diff
// on stdout only when the manifests are untidy, verify names each module whose
// download was modified, and either reports a SECURITY ERROR when a download
// disagrees with go.sum. Anything else, such as an unreachable module proxy, is
// an error and never a pass. The native output is kept, as commandFindings
// keeps it. runner/checks.go keeps a copy for the Dagger path; change both
// together.
func modFindings(step modStep, module string, exitCode int, stdout, stderr string) ([]finding, error) {
	if exitCode == 0 {
		return nil, nil
	}

	message := strings.TrimSpace(stdout + "\n" + stderr)
	mismatch := strings.Contains(stderr, "SECURITY ERROR")
	switch step {
	case modTidy:
		mismatch = mismatch || strings.TrimSpace(stdout) != ""
	case modVerify:
		mismatch = mismatch || modifiedModule.MatchString(stderr)
	}
	if exitCode != 1 || !mismatch {
		return nil, fmt.Errorf("go mod %s exited %d: %s", step, exitCode, message)
	}
	return []finding{{
		Code:     string(CheckGoMod),
		Message:  message,
		Location: location{File: module, Line: 1},
	}}, nil
}

// findingsDetails encodes diagnostics into the Details envelope the Dagger path
// produces, so consumers read one shape.
func findingsDetails(findings []finding) json.RawMessage {
	details, err := json.Marshal(struct {
		Findings []finding `json:"findings"`
	}{findings})
	if err != nil {
		return nil
	}
	return details
}
