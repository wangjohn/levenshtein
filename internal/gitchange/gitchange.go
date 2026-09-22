// Package gitchange finds what a branch changed relative to its base, for checks
// that judge only the change rather than the whole tree.
package gitchange

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Runner runs one git binary in one directory with a fixed environment.
type Runner struct {
	Git string
	Dir string
	Env []string
}

// Run returns git's stdout. A failure carries git's stderr, and a cancelled
// context reports the cancellation rather than the killed process.
func (g Runner) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.Git, append([]string{"-c", "core.quotePath=false"}, args...)...)
	cmd.Dir = g.Dir
	cmd.Env = g.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ResolveBase accepts a local branch name and falls back to its origin tracking
// ref. It returns the ref it used and the merge base of that ref with HEAD.
func (g Runner) ResolveBase(ctx context.Context, base string) (string, string, error) {
	for _, ref := range []string{base, "origin/" + base} {
		if _, err := g.Run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			mergeBase, err := g.Run(ctx, "merge-base", ref, "HEAD")
			if err != nil {
				return "", "", err
			}
			return ref, strings.TrimSpace(mergeBase), nil
		}
	}
	if shallow, err := g.Run(ctx, "rev-parse", "--is-shallow-repository"); err == nil && strings.TrimSpace(shallow) == "true" {
		return "", "", fmt.Errorf("base branch %q was not found and the checkout is shallow; fetch the base branch or check out with fetch-depth: 0", base)
	}
	return "", "", fmt.Errorf("base branch %q was not found locally or as origin/%s; fetch it before running this check", base, base)
}

// SourcePrefix maps git's toplevel-relative paths onto a source directory
// inside the worktree. The prefix is empty or ends in a slash.
func SourcePrefix(top, source string) (string, error) {
	top, topErr := filepath.EvalSymlinks(top)
	source, sourceErr := filepath.EvalSymlinks(source)
	if topErr != nil || sourceErr != nil {
		return "", fmt.Errorf("cannot resolve git worktree %q for source %q", top, source)
	}
	rel, err := filepath.Rel(top, source)
	if err != nil || !filepath.IsLocal(rel) && rel != "." {
		return "", fmt.Errorf("source %q is outside the git worktree %q", source, top)
	}
	if rel == "." {
		return "", nil
	}
	return filepath.ToSlash(rel) + "/", nil
}

// Paths is what a branch changed: the base it was compared with and the
// source-relative, slash-separated paths that exist in the working tree.
type Paths struct {
	BaseRef string
	Base    string
	Paths   []string
}

// Changed lists files the working tree adds or modifies relative to the merge
// base with base, plus untracked files that are not ignored. Deleted files are
// left out because nothing of them remains to check. Paths outside source are
// dropped, and the rest are relative to source.
func (g Runner) Changed(ctx context.Context, source, base string) (Paths, error) {
	top, err := g.Run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return Paths{}, err
	}
	prefix, err := SourcePrefix(strings.TrimSpace(top), source)
	if err != nil {
		return Paths{}, err
	}
	ref, mergeBase, err := g.ResolveBase(ctx, base)
	if err != nil {
		return Paths{}, err
	}

	// Both listings are toplevel-relative, so one prefix maps them onto source.
	diffed, err := g.Run(ctx, "diff", "--name-only", "--no-renames", "--no-ext-diff", "--diff-filter=d", "-z", mergeBase, "--")
	if err != nil {
		return Paths{}, err
	}
	untracked, err := g.Run(ctx, "ls-files", "--others", "--exclude-standard", "--full-name", "-z", ":/")
	if err != nil {
		return Paths{}, err
	}

	var paths []string
	for _, path := range slices.Concat(strings.Split(diffed, "\x00"), strings.Split(untracked, "\x00")) {
		if rel, ok := strings.CutPrefix(path, prefix); ok && rel != "" {
			paths = append(paths, rel)
		}
	}
	slices.Sort(paths)
	return Paths{BaseRef: ref, Base: mergeBase, Paths: slices.Compact(paths)}, nil
}
