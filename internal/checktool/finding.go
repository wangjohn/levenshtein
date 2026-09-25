package checktool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Finding is one diagnostic, in the shape the runner attaches as its
// levenshteinFindings extension and the native executor reports, so a report
// says the same thing however the check was executed.
type Finding struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
	// Source names the rule module a community finding came from, as
	// path@version; core findings leave it out.
	Source string `json:"source,omitempty"`
	// URL documents the rule that reported the finding, when it has a page.
	URL string `json:"url,omitempty"`
	// Advisory findings are reported without failing the check.
	Advisory bool `json:"advisory"`
}

type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Run is one finished tool process.
type Run struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Kind is a shared check kind. A check that keeps a tool's whole report as one
// finding codes it with its kind.
type Kind string

// The kinds whose results this package reads differently from the rest.
const (
	KindGoMod            Kind = "go-mod"
	KindGoTest           Kind = "go-test"
	KindGoVuln           Kind = "go-vuln"
	KindWorkflowSecurity Kind = "workflow-security"
	KindDepsVuln         Kind = "deps-vuln"
)

// ToolExit preserves a standalone tool's own output, including its precise
// locations, as one finding. Only the tool's documented failure code means
// diagnostics; every other nonzero exit is a tool error.
func ToolExit(kind Kind, module string, run Run) ([]Finding, error) {
	if run.ExitCode == 0 {
		return nil, nil
	}

	message := output(run)
	failureCode := 1
	if kind == KindGoVuln {
		failureCode = 3 // govulncheck distinguishes vulnerabilities from tool errors.
	}
	if run.ExitCode != failureCode || message == "" {
		return nil, fmt.Errorf("%s exited %d: %s", kind, run.ExitCode, message)
	}
	return whole(kind, module, message), nil
}

// whole is a check's one finding for a report that has no locations of its
// own, at the module it checked.
func whole(kind Kind, module, message string) []Finding {
	return []Finding{{
		Code:     string(kind),
		Message:  message,
		Location: Location{File: module, Line: 1},
	}}
}

// output is everything a tool printed, stdout first.
func output(run Run) string {
	return strings.TrimSpace(run.Stdout + "\n" + run.Stderr)
}

// relative reports a tool's file location relative to root, the directory the
// tool ran over, so the same diagnostic reads the same whichever executor
// produced it. A path outside root, or a relative one, is left alone.
func relative(root, file string) string {
	if root == "" || !filepath.IsAbs(file) {
		return filepath.ToSlash(file)
	}

	rel, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(rel) {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(rel)
}
