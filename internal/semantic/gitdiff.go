package semantic

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FileKind groups changed files by the questions that apply to them.
type FileKind string

const (
	FileSource   FileKind = "source"
	FileTest     FileKind = "test"
	FileDocs     FileKind = "docs"
	FileConfig   FileKind = "config"
	FileWorkflow FileKind = "workflow"
	FileOther    FileKind = "other"
)

// FileStatus is the diff status of a changed path.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileDeleted  FileStatus = "deleted"
)

// Hunk is one zero-context hunk. Every line in the new range was added.
type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Header   string
	Diff     string
	Added    []string
}

type FileChange struct {
	Path    string
	Kind    FileKind
	Status  FileStatus
	Hunks   []Hunk
	Added   int
	Removed int
}

// Commit is what the change-scope questions see about one commit.
type Commit struct {
	SHA         string   `json:"sha"`
	Subject     string   `json:"subject"`
	Files       []string `json:"files"`
	HunkHeaders []string `json:"hunk_headers"`
}

// Change is the working tree compared with the merge base of the configured branch.
type Change struct {
	BaseRef string
	Base    string
	Files   []FileChange
	Commits []Commit
}

type gitRunner struct {
	Git string
	Dir string
	Env []string
}

func (g gitRunner) run(ctx context.Context, args ...string) (string, error) {
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

// resolveBase accepts a local branch name and falls back to its origin tracking ref.
func (g gitRunner) resolveBase(ctx context.Context, base string) (string, string, error) {
	for _, ref := range []string{base, "origin/" + base} {
		if _, err := g.run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			mergeBase, err := g.run(ctx, "merge-base", ref, "HEAD")
			if err != nil {
				return "", "", err
			}
			return ref, strings.TrimSpace(mergeBase), nil
		}
	}
	if shallow, err := g.run(ctx, "rev-parse", "--is-shallow-repository"); err == nil && strings.TrimSpace(shallow) == "true" {
		return "", "", fmt.Errorf("base branch %q was not found and the checkout is shallow; fetch the base branch or check out with fetch-depth: 0", base)
	}
	return "", "", fmt.Errorf("base branch %q was not found locally or as origin/%s; fetch it before running semantic-lint", base, base)
}

const maxCommits = 50

// diffOptions pin the output the parser expects. The prefixes matter as much
// as the zero context: diff.mnemonicPrefix in a user's configuration emits c/
// and w/ instead of a/ and b/, and every path would then keep its prefix.
var diffOptions = []string{"--unified=0", "--no-color", "--no-renames", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}

func diffArgs(command string, rest ...string) []string {
	args := append([]string{command}, diffOptions...)
	return append(args, rest...)
}

func loadChange(ctx context.Context, g gitRunner, base string, include func(string) bool) (Change, error) {
	ref, mergeBase, err := g.resolveBase(ctx, base)
	if err != nil {
		return Change{}, err
	}

	raw, err := g.run(ctx, diffArgs("diff", mergeBase, "--")...)
	if err != nil {
		return Change{}, err
	}
	files, err := parseDiff(raw, include)
	if err != nil {
		return Change{}, err
	}

	untracked, err := g.run(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Change{}, err
	}
	for path := range strings.SplitSeq(untracked, "\x00") {
		if path == "" || !include(path) {
			continue
		}
		file, ok := untrackedFile(g.Dir, path)
		if ok {
			files = append(files, file)
		}
	}

	commits, err := g.commits(ctx, mergeBase)
	if err != nil {
		return Change{}, err
	}
	return Change{BaseRef: ref, Base: mergeBase, Files: files, Commits: commits}, nil
}

// commitRecord opens each record in the single log pass. A NUL and a record
// separator cannot appear in a subject, and git's text patches never contain
// them either, so one call carries every subject with its hunks.
const (
	commitRecord = "\x00\x1ecommit\x1f"
	commitFormat = "%x00%x1ecommit%x1f%H%x1f%s"
)

// commits reads every commit since the merge base in one pass. The patch comes
// along because commit_subject_matches compares each subject against the files
// and hunk headers that commit touched.
func (g gitRunner) commits(ctx context.Context, mergeBase string) ([]Commit, error) {
	args := diffArgs("log", "-p", "--no-merges", "--max-count="+strconv.Itoa(maxCommits), "--format="+commitFormat, mergeBase+"..HEAD", "--")
	log, err := g.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseCommitLog(log)
}

// parseCommitLog keeps only what the change state uses: the record lines, the
// file headers, and the @@ lines. Patch bodies are skipped, so an added line
// that itself begins with +++ cannot be read as a file header.
func parseCommitLog(raw string) ([]Commit, error) {
	var commits []Commit
	var current *Commit
	var path string
	inHunk := false

	flushPath := func() {
		if current != nil && path != "" {
			current.Files = append(current.Files, path)
		}
		path = ""
	}
	flushCommit := func() {
		flushPath()
		if current != nil {
			commits = append(commits, *current)
			current = nil
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, commitRecord):
			flushCommit()
			sha, subject, ok := strings.Cut(strings.TrimPrefix(line, commitRecord), "\x1f")
			if !ok || len(sha) < 12 {
				continue
			}
			commit := Commit{SHA: sha[:12], Subject: subject}
			current, inHunk = &commit, false
		case current == nil:
			continue
		case strings.HasPrefix(line, "diff --git "):
			flushPath()
			inHunk = false
		case inHunk && !strings.HasPrefix(line, "@@ "):
			continue
		case strings.HasPrefix(line, "@@ "):
			inHunk = true
			if hunk, ok := parseHunkHeader(line); ok && path != "" {
				current.HunkHeaders = append(current.HunkHeaders, fmt.Sprintf("%s @@ -%d,%d +%d,%d @@ %s", path, hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines, hunk.Header))
			}
		case strings.HasPrefix(line, "--- "):
			if name := strings.TrimPrefix(line, "--- "); path == "" && name != "/dev/null" {
				path = strings.TrimPrefix(name, "a/")
			}
		case strings.HasPrefix(line, "+++ "):
			if name := strings.TrimPrefix(line, "+++ "); name != "/dev/null" {
				path = strings.TrimPrefix(name, "b/")
			}
		}
	}
	flushCommit()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading commit log: %w", err)
	}
	return commits, nil
}

const maxUntrackedBytes = 256 << 10

// untrackedFile presents a new, not yet added file as a single all-added hunk.
func untrackedFile(dir, path string) (FileChange, bool) {
	kind := classify(path)
	if kind == FileOther || kind == FileConfig || kind == FileWorkflow {
		return FileChange{}, false
	}
	info, err := os.Lstat(filepath.Join(dir, path))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxUntrackedBytes {
		return FileChange{}, false
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return FileChange{}, false
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var diff strings.Builder
	fmt.Fprintf(&diff, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		diff.WriteString("+" + line + "\n")
	}
	hunk := Hunk{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: len(lines), Diff: diff.String(), Added: lines}
	return FileChange{Path: path, Kind: kind, Status: FileAdded, Hunks: []Hunk{hunk}, Added: len(lines)}, true
}

var kindByExtension = map[string]FileKind{
	".go":   FileSource,
	".md":   FileDocs,
	".json": FileConfig,
	".yaml": FileConfig,
	".yml":  FileConfig,
	".toml": FileConfig,
}

func classify(path string) FileKind {
	slash := filepath.ToSlash(path)
	if strings.HasPrefix(slash, ".github/workflows/") {
		return FileWorkflow
	}
	if strings.HasSuffix(slash, "_test.go") {
		return FileTest
	}
	if kind, ok := kindByExtension[filepath.Ext(slash)]; ok {
		return kind
	}
	return FileOther
}

// parseDiff reads zero-context unified output. Binary files carry no hunks and are skipped.
func parseDiff(raw string, include func(string) bool) ([]FileChange, error) {
	var files []FileChange
	var path string
	var status FileStatus
	var hunks []Hunk
	var added, removed int
	var current *Hunk
	binary := false
	sawHunk := false

	flush := func() {
		if current != nil {
			hunks = append(hunks, *current)
			current = nil
		}
		if path != "" && !binary && include(path) && (len(hunks) > 0 || status == FileDeleted) {
			files = append(files, FileChange{Path: path, Kind: classify(path), Status: status, Hunks: hunks, Added: added, Removed: removed})
		}
		path, status, hunks, added, removed, binary, sawHunk = "", FileModified, nil, 0, 0, false, false
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
		case strings.HasPrefix(line, "new file mode"):
			status = FileAdded
		case strings.HasPrefix(line, "deleted file mode"):
			status = FileDeleted
		case strings.HasPrefix(line, "Binary files "):
			binary = true
		// File headers only precede the first hunk. Inside a hunk, a line that
		// starts with "+++ " or "--- " is content (an added "++ x" or a removed
		// "-- x") and must not rename the file or vanish from the counts.
		case !sawHunk && strings.HasPrefix(line, "--- "):
			if name := strings.TrimPrefix(line, "--- "); path == "" && name != "/dev/null" {
				path = strings.TrimPrefix(name, "a/")
			}
		case !sawHunk && strings.HasPrefix(line, "+++ "):
			if name := strings.TrimPrefix(line, "+++ "); name != "/dev/null" {
				path = strings.TrimPrefix(name, "b/")
			}
		case strings.HasPrefix(line, "@@ "):
			sawHunk = true // Even a malformed header ends the header region.
			if current != nil {
				hunks = append(hunks, *current)
			}
			hunk, ok := parseHunkHeader(line)
			if !ok {
				current = nil
				continue
			}
			current = &hunk
		case current != nil && strings.HasPrefix(line, "+"):
			current.Diff += line + "\n"
			current.Added = append(current.Added, line[1:])
			added++
		case current != nil && strings.HasPrefix(line, "-"):
			current.Diff += line + "\n"
			removed++
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading diff: %w", err)
	}
	return files, nil
}

func parseHunkHeader(line string) (Hunk, bool) {
	rest := strings.TrimPrefix(line, "@@ ")
	ranges, header, ok := strings.Cut(rest, " @@")
	if !ok {
		return Hunk{}, false
	}
	old, updated, ok := strings.Cut(ranges, " +")
	if !ok || !strings.HasPrefix(old, "-") {
		return Hunk{}, false
	}
	oldStart, oldLines, ok := parseRange(strings.TrimPrefix(old, "-"))
	if !ok {
		return Hunk{}, false
	}
	newStart, newLines, ok := parseRange(updated)
	if !ok {
		return Hunk{}, false
	}
	return Hunk{OldStart: oldStart, OldLines: oldLines, NewStart: newStart, NewLines: newLines, Header: strings.TrimSpace(header), Diff: line + "\n"}, true
}

func parseRange(value string) (int, int, bool) {
	start, count, hasCount := strings.Cut(value, ",")
	first, err := strconv.Atoi(start)
	if err != nil {
		return 0, 0, false
	}
	lines := 1
	if hasCount {
		lines, err = strconv.Atoi(count)
		if err != nil {
			return 0, 0, false
		}
	}
	return first, lines, true
}
