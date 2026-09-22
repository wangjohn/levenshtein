package verify

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// finding and location mirror the diagnostic shape the Dagger module attaches
// as its levenshteinFindings extension, so a report says the same thing however
// the check was executed. The runner is a separate Go module and cannot be
// imported, so these are a deliberate copy; change both together.
type finding struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location location `json:"location"`
}

type location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// allowedCode reports whether a diagnostic code is one the configured check
// list selects. A code is accepted when it matches at least one positive
// pattern and no negated one; "all" matches every code and a leading "-"
// negates. Anything else, including a compile error reported as a diagnostic,
// is not a lint finding and makes the run an error rather than a failure.
func allowedCode(code string, checks []string) bool {
	accepted := false
	for _, check := range checks {
		pattern, negated := strings.CutPrefix(check, "-")
		matched := pattern == "all"
		if !matched {
			matched, _ = path.Match(pattern, code)
		}
		if !matched {
			continue
		}
		if negated {
			return false
		}
		accepted = true
	}
	return accepted
}

// parseFindings turns one Staticcheck run into diagnostics, refusing any result
// that does not agree with itself: an unexpected exit code, output that is not
// the configured checks, an exit status that contradicts the diagnostics, or
// anything at all on stderr.
func parseFindings(exitCode int, stdout, stderr string, checks []string, root string) ([]finding, error) {
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("Staticcheck exited %d: %s\n%s", exitCode, stderr, stdout)
	}

	var findings []finding
	decoder := json.NewDecoder(strings.NewReader(stdout))
	for {
		var diagnostic finding
		err := decoder.Decode(&diagnostic)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Staticcheck JSON: %w", err)
		}
		if !allowedCode(diagnostic.Code, checks) || diagnostic.Message == "" || diagnostic.Location.File == "" || diagnostic.Location.Line < 1 {
			return nil, fmt.Errorf("unexpected diagnostic (possibly a compile error): %s", stdout)
		}
		diagnostic.Location.File = repositoryPath(root, diagnostic.Location.File)
		findings = append(findings, diagnostic)
	}

	if (exitCode == 0 && len(findings) != 0) || (exitCode == 1 && len(findings) == 0) {
		return nil, fmt.Errorf("Staticcheck exit %d does not match diagnostics: %s\n%s", exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		return nil, fmt.Errorf("Staticcheck could not produce a clean result: %s", stderr)
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
