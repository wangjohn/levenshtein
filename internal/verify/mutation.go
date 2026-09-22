package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wangjohn/levenshtein/internal/gitchange"
)

// defaultMutationTimeout caps a whole go-mutation run. Hitting it is an error,
// never a pass, so a pathological change cannot hold CI indefinitely.
const defaultMutationTimeout = 20 * time.Minute

// mutationSelection is what the host decides before Dagger runs: the files to
// mutate, relative to the target's module directory, and a note saying where
// they came from.
type mutationSelection struct {
	Files []string
	Note  string
}

// mutationFiles chooses the files a go-mutation check mutates. Dagger receives
// the source without .git, so the changed set is computed here and passed in
// as an argument; the container never has to know about the branch.
func mutationFiles(ctx context.Context, req Request) (mutationSelection, error) {
	options := req.Check.mutationOptions()
	moduleDir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return mutationSelection{}, err
	}

	var candidates []string
	var note string
	switch options.Scope {
	case MutationScopeModule:
		candidates, err = moduleGoFiles(req.Source, req.Target.Dir)
		if err != nil {
			return mutationSelection{}, err
		}
		note = "every Go file in " + req.Target.Dir
	case MutationScopeChanged:
		git, err := exec.LookPath("git")
		if err != nil {
			return mutationSelection{}, fmt.Errorf("go-mutation needs git on PATH to find the changed files: %w", err)
		}
		changed, err := gitchange.Runner{Git: git, Dir: req.Source, Env: os.Environ()}.Changed(ctx, req.Source, changeBase(options.Base))
		if err != nil {
			return mutationSelection{}, err
		}
		for _, path := range changed.Paths {
			candidates = append(candidates, filepath.FromSlash(path))
		}
		note = "Go files changed since " + changed.BaseRef
	default:
		return mutationSelection{}, fmt.Errorf("unsupported go-mutation scope %q", options.Scope)
	}

	var files []string
	for _, path := range candidates {
		if !mutable(req, moduleDir, path) {
			continue
		}
		rel, err := filepath.Rel(req.Target.Dir, path)
		if err != nil {
			return mutationSelection{}, err
		}
		files = append(files, filepath.ToSlash(rel))
	}
	slices.Sort(files)
	return mutationSelection{Files: files, Note: note}, nil
}

// acceptedReachable rejects an accepted-survivors file that exists but that the
// Dagger source would leave out, which would otherwise accept nothing silently.
func acceptedReachable(req Request, accepted string) error {
	_, statErr := os.Stat(filepath.Join(req.Source, accepted))
	if errors.Is(statErr, fs.ErrNotExist) {
		return nil // A missing file accepts nothing, which is the intended default.
	}
	covered := slices.ContainsFunc(req.Target.Inputs, func(input string) bool { return input == "." || within(accepted, input) })
	if !covered || excluded(accepted, req.Target.Exclude) {
		return fmt.Errorf("accepted survivors file %q is outside the target's inputs, so the check cannot read it; add it to inputs", accepted)
	}
	return nil
}

// changeBase picks the branch a change is measured against: the configured
// base, then the pull request's base in GitHub Actions, then main.
func changeBase(configured string) string {
	if configured != "" {
		return configured
	}
	if base := os.Getenv(baseRefEnv); gitRef(base) && base != "" {
		return base
	}
	return defaultBaseRef
}

// mutable reports whether a source-relative path is a hand-written, non-test
// Go file of the target's own module that the target's inputs cover.
func mutable(req Request, moduleDir, path string) bool {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || privateSourcePath(path) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if slices.Contains(parts, "testdata") || slices.Contains(parts, "vendor") {
		return false
	}
	if req.Target.Dir != "." && !within(path, req.Target.Dir) {
		return false
	}
	if !slices.ContainsFunc(req.Target.Inputs, func(input string) bool { return input == "." || within(path, input) }) {
		return false
	}
	if excluded(path, req.Target.Exclude) {
		return false
	}

	full := filepath.Join(req.Source, path)
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if nestedModule(moduleDir, filepath.Dir(full)) {
		return false
	}
	return !generated(full)
}

// nestedModule reports whether dir belongs to another module inside moduleDir,
// which its own go.mod would claim. Mutating it would run the wrong tests.
func nestedModule(moduleDir, dir string) bool {
	for dir != moduleDir && strings.HasPrefix(dir, moduleDir+string(filepath.Separator)) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return true
		}
		dir = filepath.Dir(dir)
	}
	return false
}

// generated reports whether a Go file carries the standard generated-code
// header. A file that does not parse is left in, so gremlins reports on it.
func generated(path string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly|parser.ParseComments)
	return err == nil && ast.IsGenerated(file)
}

// moduleGoFiles lists source-relative .go files under the target's directory,
// skipping directories that can never hold the module's own mutable code.
func moduleGoFiles(source, dir string) ([]string, error) {
	var files []string
	root := filepath.Join(source, dir)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "testdata" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

// mutationSummary is what the runner reports for a completed run, whether it
// passed or not, so an uncovered line is visible even when nothing failed.
type mutationSummary struct {
	Killed     int              `json:"killed"`
	Lived      int              `json:"lived"`
	Accepted   int              `json:"accepted"`
	NotCovered int              `json:"not_covered"`
	TimedOut   int              `json:"timed_out"`
	NotViable  int              `json:"not_viable"`
	Skipped    int              `json:"skipped"`
	Uncovered  []mutationMutant `json:"uncovered,omitempty"`
	Files      []string         `json:"files"`
}

type mutationMutant struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Mutator string `json:"mutator"`
}

// mutationSummaryOf finds the runner's summary: the function's return value on
// a pass, or the summary a failing run attached to its details.
func mutationSummaryOf(result Result, returned string) string {
	if returned != "" {
		return returned
	}
	var details struct {
		Summary json.RawMessage `json:"summary"`
	}
	if json.Unmarshal(result.Details, &details) != nil {
		return ""
	}
	return string(details.Summary)
}

// mutationStdout turns the runner's summary into the line a person reads. A
// passing run has no details yet, so the summary becomes them.
func mutationStdout(raw, note string, details json.RawMessage) (string, json.RawMessage) {
	var summary mutationSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return note, details
	}
	if details == nil {
		details, _ = json.Marshal(struct {
			Summary json.RawMessage `json:"summary"`
		}{json.RawMessage(raw)})
	}
	line := fmt.Sprintf("%s: %d files mutated; %d killed, %d survived, %d accepted, %d not covered, %d timed out",
		note, len(summary.Files), summary.Killed, summary.Lived, summary.Accepted, summary.NotCovered, summary.TimedOut)
	return line, details
}

// rawJSON keeps a summary only when it is valid JSON, so a malformed one
// cannot corrupt the result's details.
func rawJSON(value string) json.RawMessage {
	if value == "" || !json.Valid([]byte(value)) {
		return nil
	}
	return json.RawMessage(value)
}
