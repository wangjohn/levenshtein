package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/config"
)

// LV1005 checks every Go file gofmt -l would, including the ones a build
// leaves out: another platform's files and files behind a build tag such as
// integration or ignore. No analyzer sees those, and Staticcheck keys a
// package's cached results on the files its build compiles, so an analyzer
// that read them would keep a stale finding after one was formatted. The
// linter checks them after Staticcheck's run instead, outside its cache, and
// prints each finding the way Staticcheck prints LV1005's own.

// outputFormat is lintcmd's -f flag.
type outputFormat string

// The output formats the pass over excluded files can add lines to: lintcmd's
// text and JSON formatters write one line per finding. Stylish, SARIF, and
// binary output are whole documents, and null prints nothing, so those runs
// leave excluded files unchecked.
const (
	textFormat outputFormat = "text"
	jsonFormat outputFormat = "json"
)

// severity is how lintcmd marks a finding: an error fails the run, and a
// warning, a finding of a check that -fail leaves out, does not.
type severity string

const (
	severityError   severity = "error"
	severityWarning severity = "warning"
)

// checkExcluded reports the unformatted Go files that the packages matching
// the command-line patterns leave out of their build, given the status of
// Staticcheck's run, and returns the run's status. checks is the -checks
// selection, or nil to let each directory's staticcheck.conf decide.
func checkExcluded(flags *flag.FlagSet, checks []string, status int, stdout, stderr io.Writer) int {
	format := outputFormat(flags.Lookup("f").Value.String())
	if (format != textFormat && format != jsonFormat) || flags.Lookup("matrix").Value.String() == "true" {
		return status
	}

	tests := flags.Lookup("tests").Value.String() == "true"
	names, err := excludedFiles(flags.Lookup("tags").Value.String(), tests, flags.Args())
	if err != nil {
		toolError(stderr, err)
		if status != 0 {
			return status
		}
		return 2
	}

	level := severityError
	if !allowed(flagList(flags, "fail"), policy.Formatting.Name) {
		level = severityWarning
	}
	for _, name := range names {
		reported, err := checkFile(checks, name)
		if err != nil {
			toolError(stderr, fmt.Errorf("%s: %w", name, err))
			return 2
		}
		if !reported {
			continue
		}

		if err := printFinding(stdout, format, level, name); err != nil {
			toolError(stderr, err)
			return 2
		}
		if level == severityError && status == 0 {
			status = 1
		}
	}
	return status
}

// toolError reports why excluded files could not be checked. The run's exit
// status says it failed, so an error writing the message is dropped.
func toolError(stderr io.Writer, err error) {
	_, _ = fmt.Fprintf(stderr, "levenshtein-lint: LV1005 could not check excluded files: %s\n", err)
}

// checkFile reports whether LV1005 reports an excluded file: the rule is
// selected for its directory, and the file is unformatted.
func checkFile(checks []string, name string) (bool, error) {
	enabled, err := selected(checks, filepath.Dir(name))
	if err != nil || !enabled {
		return false, err
	}
	return unformattedFile(name)
}

// selected reports whether LV1005 runs on the files in dir: the -checks
// selection when there is one, and otherwise dir's staticcheck.conf, as
// Staticcheck decides for a package.
func selected(checks []string, dir string) (bool, error) {
	if checks != nil {
		return allowed(checks, policy.Formatting.Name), nil
	}
	loaded, err := config.Load(dir)
	if err != nil {
		return false, err
	}
	return allowed(loaded.Checks, policy.Formatting.Name), nil
}

// excludedFiles lists, in order, every Go file that go list reports as left
// out of the packages matching patterns under the run's build tags, with the
// default build context otherwise, as Staticcheck loads them. Test files are
// left out too when the run checks no tests (-tests=false).
func excludedFiles(tags string, tests bool, patterns []string) ([]string, error) {
	args := []string{"list", "-e", "-json=Dir,IgnoredGoFiles"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	var stderr bytes.Buffer
	list := exec.CommandContext(context.Background(), "go", append(args, patterns...)...)
	list.Stderr = &stderr
	listing, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}

	var names []string
	decoder := json.NewDecoder(bytes.NewReader(listing))
	for {
		var pkg struct {
			Dir            string   `json:"Dir"`
			IgnoredGoFiles []string `json:"IgnoredGoFiles"`
		}
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading go list output: %w", err)
		}
		for _, base := range pkg.IgnoredGoFiles {
			if !tests && strings.HasSuffix(base, "_test.go") {
				continue
			}
			names = append(names, filepath.Join(pkg.Dir, base))
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// unformattedFile reports whether LV1005 reports an excluded file: one that is
// not generated, by the rule the analyzer uses, not suppressed by a directive,
// and not what gofmt writes. A file that does not parse counts as formatted,
// as it does for the analyzer: gofmt cannot format it either.
func unformattedFile(name string) (bool, error) {
	source, err := os.ReadFile(name)
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, name, source, parser.ParseComments|parser.SkipObjectResolution)
	if parseErr != nil || policy.Generated(fset, file) || ignored(fset, file) {
		return false, nil
	}
	return !policy.Formatted(source), nil
}

// ignored reports whether a directive suppresses LV1005's finding at the start
// of a file, as Staticcheck matches one for a compiled file: a
// //lint:file-ignore, or a //lint:ignore on the node at line 1, that names the
// rule by a case-insensitive glob and gives a reason.
func ignored(fset *token.FileSet, file *ast.File) bool {
	for _, directive := range lint.ParseDirectives([]*ast.File{file}, fset) {
		// Staticcheck reports a directive without a reason and applies none.
		wholeFile := directive.Command == "file-ignore"
		firstLine := directive.Command == "ignore" && fset.Position(directive.Node.Pos()).Line == 1
		if len(directive.Arguments) < 2 || (!wholeFile && !firstLine) {
			continue
		}
		for check := range strings.SplitSeq(directive.Arguments[0], ",") {
			if matched, _ := filepath.Match(strings.ToLower(check), strings.ToLower(policy.Formatting.Name)); matched {
				return true
			}
		}
	}
	return false
}

// printFinding prints LV1005's finding for the start of a file exactly as
// lintcmd's text or JSON formatter does (lintcmd/format.go in
// honnef.co/go/tools v0.8.1). An analyzer's finding with no end position has
// an empty end.
func printFinding(w io.Writer, format outputFormat, level severity, name string) error {
	if format == textFormat {
		_, err := fmt.Fprintf(w, "%s:1:1: %s (%s)\n", shortPath(name), policy.UnformattedMessage, policy.Formatting.Name)
		return err
	}

	type location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	}
	type finding struct {
		Code     string   `json:"code"`
		Severity severity `json:"severity,omitempty"`
		Location location `json:"location"`
		End      location `json:"end"`
		Message  string   `json:"message"`
	}
	return json.NewEncoder(w).Encode(finding{
		Code:     policy.Formatting.Name,
		Severity: level,
		Location: location{
			File:   name,
			Line:   1,
			Column: 1,
		},
		Message: policy.UnformattedMessage,
	})
}

// shortPath names a file relative to the working directory when that is
// shorter, as lintcmd's text formatter does.
func shortPath(path string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(cwd, path); err == nil && len(rel) < len(path) {
		return rel
	}
	return path
}
