package verify

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
	"github.com/wangjohn/levenshtein/internal/gitchange"
)

// helperApidiff is the pinned apidiff that levenshtein-gocheck drives.
var helperApidiff = helper{Name: "apidiff", Module: "runner/tools", Pkg: "golang.org/x/exp/cmd/apidiff"}

// apidiffBaseTree is the target's declared inputs as they were at the merge
// base with the base branch, exported on the host because neither the Dagger
// source nor the check itself has the repository's history.
type apidiffBaseTree struct {
	// Dir holds the exported files, laid out as in the source.
	Dir string
	// Ref is the branch the merge base was taken with, as it resolved.
	Ref string
	// Commit is the merge base.
	Commit string
	// Missing is true when the target's module did not exist at the base.
	Missing bool
}

// note says what the comparison was made with.
func (b apidiffBaseTree) note() string {
	commit := b.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return fmt.Sprintf("compared with the merge base of %s, %s", b.Ref, commit)
}

// apidiffBase finds the merge base of HEAD with the check's base branch and
// exports the target's declared inputs at that commit into a temporary
// directory, less the target's excludes and private files. release removes it.
func apidiffBase(ctx context.Context, req Request) (apidiffBaseTree, func(), error) {
	release := func() {}
	git, err := exec.LookPath("git")
	if err != nil {
		return apidiffBaseTree{}, release, fmt.Errorf("go-apidiff needs git on PATH to find the base branch: %w", err)
	}
	runner := gitchange.Runner{Git: git, Dir: req.Source, Env: os.Environ()}
	top, err := runner.Run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return apidiffBaseTree{}, release, err
	}
	top = strings.TrimSpace(top)
	prefix, err := gitchange.SourcePrefix(top, req.Source)
	if err != nil {
		return apidiffBaseTree{}, release, err
	}
	ref, commit, err := runner.ResolveBase(ctx, changeBase(req.Check.apidiffOptions().Base))
	if err != nil {
		return apidiffBaseTree{}, release, err
	}

	// Pathspecs are literal, and relative to the top of the work tree.
	atTop := gitchange.Runner{Git: git, Dir: top, Env: os.Environ()}
	exists := func(rel string) (bool, error) {
		listed, err := atTop.Run(ctx, "--literal-pathspecs", "ls-tree", "--name-only", commit, "--", gitPath(prefix, rel))
		return strings.TrimSpace(listed) != "", err
	}
	module, err := exists(filepath.Join(req.Target.Dir, "go.mod"))
	if err != nil {
		return apidiffBaseTree{}, release, err
	}
	base := apidiffBaseTree{Ref: ref, Commit: commit, Missing: !module}
	if base.Missing {
		return base, release, nil
	}

	var paths []string
	for _, input := range req.Target.Inputs {
		found, err := exists(input)
		if err != nil {
			return apidiffBaseTree{}, release, err
		}
		if found || input == "." {
			paths = append(paths, gitPath(prefix, input))
		}
	}
	dir, err := os.MkdirTemp("", "levenshtein-apidiff-base-")
	if err != nil {
		return apidiffBaseTree{}, release, err
	}
	release = func() { _ = os.RemoveAll(dir) }
	base.Dir = dir
	if err := exportTree(ctx, git, top, commit, prefix, paths, req.Target.Exclude, dir); err != nil {
		release()
		return apidiffBaseTree{}, func() {}, err
	}
	return base, release, nil
}

// gitPath is a source-relative path as git names it from the top of the work
// tree.
func gitPath(prefix, rel string) string {
	if rel == "." {
		if prefix == "" {
			return "."
		}
		return strings.TrimSuffix(prefix, "/")
	}
	return prefix + filepath.ToSlash(rel)
}

// exportTree extracts paths at commit from git archive into dest, relative to
// the source rather than the top of the work tree. Only directories and
// regular files are written; links, which a Dagger input may not hold anyway,
// and anything outside the source, excluded, or private are skipped.
func exportTree(ctx context.Context, git, top, commit, prefix string, paths, excludes []string, dest string) error {
	args := append([]string{"--literal-pathspecs", "archive", "--format=tar", commit, "--"}, paths...)
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Dir = top
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	readErr := extractTar(tar.NewReader(stdout), prefix, excludes, dest)
	if readErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return readErr
}

func extractTar(archive *tar.Reader, prefix string, excludes []string, dest string) error {
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading git archive: %w", err)
		}
		name, ok := strings.CutPrefix(strings.TrimSuffix(header.Name, "/"), prefix)
		rel := filepath.FromSlash(name)
		if !ok || name == "" || !filepath.IsLocal(rel) || privateSourcePath(rel) || excluded(rel, excludes) {
			continue
		}

		target := filepath.Join(dest, rel)
		//exhaustive:ignore Links, devices and pax headers are not source.
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeArchived(archive, target, header.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		}
	}
}

func writeArchived(from io.Reader, target string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, from); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// workspaceRel is the go.work the head is analyzed with, relative to the
// source, or "off"; the base uses the same file when it had one.
func workspaceRel(req Request, dir string) string {
	gowork := workspace(req, dir)
	if gowork == "off" {
		return gowork
	}
	rel, err := filepath.Rel(req.Source, gowork)
	if err != nil || !filepath.IsLocal(rel) {
		return "off"
	}
	return filepath.ToSlash(rel)
}

// apidiff exports the base tree, runs levenshtein-gocheck's apidiff mode on it
// and the source, and reports what it found with the comparison it made.
func (n *Native) apidiff(ctx context.Context, req Request, dir string, env []string) Result {
	base, release, err := apidiffBase(ctx, req)
	defer release()
	if err != nil {
		if ctx.Err() != nil {
			return Result{Status: StatusCancelled, Error: ctx.Err().Error()}
		}
		return Result{Status: StatusError, Error: err.Error()}
	}
	if base.Missing {
		return apidiffResult(Result{Status: StatusPassed}, missingModuleNotes(req), base)
	}

	run := func(n *Native, ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
		return n.goApidiff(ctx, req, work, base)
	}
	result := goCheckExecutor(run, "shared check failed")(n, ctx, req, dir, env)
	var notes []string
	if result.Status == StatusPassed || result.Status == StatusFailed {
		notes = strings.Split(result.Stdout, "\n")
	}
	return apidiffResult(result, notes, base)
}

func (n *Native) goApidiff(ctx context.Context, req Request, work goRun, base apidiffBaseTree) ([]finding, toolRun, error) {
	gocheck, err := build(ctx, req, work, helperGocheck)
	if err != nil {
		return nil, toolRun{}, err
	}
	tool, err := build(ctx, req, work, helperApidiff)
	if err != nil {
		return nil, toolRun{}, err
	}

	args := []string{gocheck, "apidiff", "-tool=" + tool, "-base=" + base.Dir, "-head=" + req.Source, "-module=" + filepath.ToSlash(req.Target.Dir), "-workspace=" + workspaceRel(req, work.Dir)}
	return gocheckRun(ctx, CheckGoApidiff, work.Dir, args, work.Env)
}

// executeApidiff exports the base tree on the host, where the history is, and
// passes it to the runner's goApidiff function beside the source. A module
// that did not exist at the base passes without starting Dagger.
func (d *Dagger) executeApidiff(parent context.Context, req Request) Result {
	base, release, err := apidiffBase(parent, req)
	defer release()
	if err != nil {
		if parent.Err() != nil {
			return Result{Status: StatusCancelled, Error: parent.Err().Error()}
		}
		return Result{Status: StatusError, Error: err.Error()}
	}
	if base.Missing {
		return apidiffResult(Result{Status: StatusPassed}, missingModuleNotes(req), base)
	}
	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}
	// Connect with the run's context, as executeTests does, so the shared
	// session outlives this check's bound.
	if err := d.connect(parent, req.Shared); err != nil {
		if parent.Err() != nil {
			return Result{Status: StatusCancelled, Error: parent.Err().Error()}
		}
		return Result{Status: StatusError, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(parent, goCheckTimeout)
	defer cancel()

	source, err := daggerSource(d.client, req.Source, req.Target.Inputs, req.Target.Exclude)
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}
	var summary string
	query := d.client.QueryBuilder().Select("levenshtein").Select(daggerFunctions[CheckGoApidiff]).
		Arg("nonce", executionNonce(req)).
		Arg("source", source).
		Arg("base", d.client.Host().Directory(base.Dir, dagger.HostDirectoryOpts{Exclude: daggerExcludes(nil)})).
		Arg("module", req.Target.Dir).
		Arg("workspace", workspaceRel(req, dir)).
		Bind(&summary)
	result := daggerResult(query.Execute(ctx))

	if err := parent.Err(); err != nil {
		return Result{Status: StatusCancelled, Error: err.Error(), Stdout: result.Stdout, Stderr: result.Stderr}
	}
	if ctx.Err() != nil {
		return Result{Status: StatusError, Error: fmt.Sprintf("go-apidiff exceeded its %s timeout", goCheckTimeout), Stdout: result.Stdout, Stderr: result.Stderr}
	}
	var notes []string
	if raw := mutationSummaryOf(result, summary); raw != "" {
		_ = json.Unmarshal([]byte(raw), &notes)
	}
	return apidiffResult(result, notes, base)
}

func missingModuleNotes(req Request) []string {
	return []string{fmt.Sprintf("module %s did not exist at the base, so it has no API to break", path.Clean(filepath.ToSlash(req.Target.Dir)))}
}

// apidiffResult reports a go-apidiff verdict the same way whichever executor
// reached it: the comparison and the notes as output, and in the details the
// findings, if any, beside a summary of the base and every note, compatible
// changes included. A run that reached no verdict keeps the tool's output.
func apidiffResult(result Result, notes []string, base apidiffBaseTree) Result {
	if result.Status != StatusPassed && result.Status != StatusFailed {
		return result
	}
	notes = append([]string{base.note()}, notes...)
	var details struct {
		Findings json.RawMessage `json:"findings,omitempty"`
	}
	if len(result.Details) != 0 {
		_ = json.Unmarshal(result.Details, &details)
	}
	encoded, err := json.Marshal(struct {
		Findings json.RawMessage `json:"findings,omitempty"`
		Summary  apidiffSummary  `json:"summary"`
	}{details.Findings, apidiffSummary{Base: base.Ref, MergeBase: base.Commit, Notes: notes}})
	if err == nil {
		result.Details = encoded
	}
	result.Stdout = strings.Join(notes, "\n")
	return result
}

// apidiffSummary is the part of a go-apidiff report a person reads whether or
// not it failed.
type apidiffSummary struct {
	Base      string   `json:"base"`
	MergeBase string   `json:"merge_base"`
	Notes     []string `json:"notes"`
}
