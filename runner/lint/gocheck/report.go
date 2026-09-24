// Package gocheck implements the shared checks that judge a whole Go module
// rather than one package at a time: go-imports, go-generate, and go-apidiff.
// Both executors run them through the levenshtein-gocheck command, so each
// verdict is decided in exactly one place.
package gocheck

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
)

// Code is the finding code a check reports.
type Code string

const (
	CodeImports  Code = "go-imports"
	CodeGenerate Code = "go-generate"
	CodeApidiff  Code = "go-apidiff"
)

// Finding has the diagnostic shape every Levenshtein check reports, so the CLI
// reads it the way it reads the linter's.
type Finding struct {
	Code     Code     `json:"code"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
}

// Location is a repository-relative, slash-separated file and a 1-based line.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Report is what one run prints on stdout: the findings, and notes a person
// should see whether or not the check failed.
type Report struct {
	Findings []Finding `json:"findings"`
	Notes    []string  `json:"notes,omitempty"`
}

// Exit codes of levenshtein-gocheck. Anything but these two, including a
// report that disagrees with its exit code, is a tool error.
const (
	ExitClean    = 0
	ExitFindings = 1
	ExitError    = 2
)

// Write prints the report and returns the exit code that matches it.
func (r Report) Write(w io.Writer) (int, error) {
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return ExitError, err
	}
	if _, err := fmt.Fprintf(w, "%s\n", data); err != nil {
		return ExitError, err
	}
	if len(r.Findings) != 0 {
		return ExitFindings, nil
	}
	return ExitClean, nil
}

// run is one subprocess invocation with its outputs kept apart.
type run struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// command runs name in dir with exactly env. A process that cannot start is an
// error; one that exits nonzero is reported through ExitCode.
func command(ctx context.Context, dir string, env []string, name string, args ...string) (run, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	result := run{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return result, nil
	case ctx.Err() != nil:
		return result, ctx.Err()
	case errors.As(err, &exit):
		result.ExitCode = exit.ExitCode()
		return result, nil
	default:
		return result, err
	}
}

// output is a failed command's text for an error message.
func (r run) output() string {
	return strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
}

// sortFindings orders findings by location, so a report does not depend on
// the order a tool or a map produced them in.
func sortFindings(findings []Finding) {
	slices.SortFunc(findings, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.Location.File, b.Location.File),
			cmp.Compare(a.Location.Line, b.Location.Line),
			cmp.Compare(a.Location.Column, b.Location.Column),
			cmp.Compare(a.Message, b.Message),
		)
	})
}

// count spells a number with its noun, such as "1 rule" or "2 rules".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// withEnv returns env with each of values replacing any entry of the same name.
func withEnv(env []string, values ...string) []string {
	out := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		replaced := false
		for _, value := range values {
			if pinned, _, _ := strings.Cut(value, "="); pinned == name {
				replaced = true
			}
		}
		if !replaced {
			out = append(out, entry)
		}
	}
	return append(out, values...)
}
