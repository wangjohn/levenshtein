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
	"strconv"
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

// Paths is what a branch changed: the base it was compared with, the
// source-relative, slash-separated paths that exist in the working tree, and
// the lines the change adds or rewrites in each diffed file. An untracked file
// has no Lines entry, because every line of it is new.
type Paths struct {
	BaseRef string
	Base    string
	Paths   []string
	Lines   map[string][]Range
}

// Range is an inclusive run of line numbers in the working-tree file.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
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

	// Zero context makes every hunk's new side exactly the lines the change
	// wrote. The prefixes are pinned because diff.mnemonicPrefix would change
	// them, and textconv is off so line numbers are the file's own. Only Go
	// files are read: a large regenerated fixture would otherwise be buffered
	// whole. In a pathspec without glob magic, * also matches /, so the one
	// pattern covers every directory from the top of the work tree.
	hunks, err := g.Run(ctx, "diff", "--unified=0", "--no-color", "--no-renames", "--no-ext-diff", "--no-textconv", "--diff-filter=d", "--src-prefix=a/", "--dst-prefix=b/", mergeBase, "--", ":(top)*.go")
	if err != nil {
		return Paths{}, err
	}

	parsed := changedLines(hunks)
	lines := map[string][]Range{}
	var paths []string
	for path := range strings.SplitSeq(diffed, "\x00") {
		rel, ok := strings.CutPrefix(path, prefix)
		if !ok || rel == "" {
			continue
		}
		paths = append(paths, rel)
		// A tracked file the diff names but whose hunks were not read, such as
		// a mode-only change or a non-Go file, changed no lines. Only an
		// untracked file counts every line as new.
		ranges := parsed[path]
		if ranges == nil {
			ranges = []Range{}
		}
		lines[rel] = ranges
	}
	for path := range strings.SplitSeq(untracked, "\x00") {
		if rel, ok := strings.CutPrefix(path, prefix); ok && rel != "" {
			paths = append(paths, rel)
		}
	}
	slices.Sort(paths)
	return Paths{BaseRef: ref, Base: mergeBase, Paths: slices.Compact(paths), Lines: lines}, nil
}

// changedLines reads the new-side range of every hunk in a zero-context diff.
// A file whose change only deletes lines still gets an entry, with no ranges,
// so it is not mistaken for an untracked file whose every line is new.
func changedLines(diff string) map[string][]Range {
	lines := map[string][]Range{}
	path := ""
	// File headers only follow a "diff --git" line; inside a hunk, an added line
	// that itself begins with "++ " would otherwise read as one.
	header := false
	for line := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			header = true
			path = ""
			continue
		}
		if name, ok := strings.CutPrefix(line, "+++ "); ok && header {
			header = false
			path = ""
			if name, ok := strings.CutPrefix(headerName(name), "b/"); ok {
				path = name
				lines[path] = []Range{}
			}
			continue
		}
		hunk, ok := strings.CutPrefix(line, "@@ ")
		if !ok || header || path == "" {
			continue
		}
		fields := strings.Fields(hunk)
		if len(fields) < 2 {
			continue
		}
		start, count, ok := hunkRange(fields[1])
		if ok && count > 0 {
			lines[path] = append(lines[path], Range{Start: start, End: start + count - 1})
		}
	}
	return lines
}

// headerName reads the path from a "+++ " header. Git ends the name with a tab
// when it contains a space, and quotes it C-style, even with core.quotePath
// off, when it contains a quote, a backslash, or a control character. Go's
// string syntax reads those escapes, octal ones included.
func headerName(name string) string {
	name = strings.TrimSuffix(name, "\t")
	if strings.HasPrefix(name, `"`) {
		if unquoted, err := strconv.Unquote(name); err == nil {
			return unquoted
		}
	}
	return name
}

// hunkRange parses a hunk header's "+start,count" side; a missing count is one.
func hunkRange(field string) (int, int, bool) {
	field, ok := strings.CutPrefix(field, "+")
	if !ok {
		return 0, 0, false
	}
	startText, countText, hasCount := strings.Cut(field, ",")
	start, err := strconv.Atoi(startText)
	if err != nil {
		return 0, 0, false
	}
	if !hasCount {
		return start, 1, true
	}
	count, err := strconv.Atoi(countText)
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}
