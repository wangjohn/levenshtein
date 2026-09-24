package gocheck

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A go-generate finding carries the file's diff, cut to a length a report can
// hold; the command that reproduces it is in the message.
const (
	maxDiffLines  = 200
	maxDiffBytes  = 16 << 10
	maxNoteBytes  = 4 << 10
	missingTool   = "executable file not found"
	generateHint  = "go-generate runs directives with the Go toolchain alone; a generator that needs another tool, such as protoc, belongs in a command check (docs/checks.md#generated-code)"
	directiveText = "//go:generate"
)

// gitStatus is one letter of git diff-tree --name-status for a file go
// generate changed. Renames are off, so these are the only letters it prints.
type gitStatus string

const (
	gitAdded    gitStatus = "A"
	gitDeleted  gitStatus = "D"
	gitModified gitStatus = "M"
	gitType     gitStatus = "T"
)

// Generate runs go generate ./... in root/module and reports every file it
// adds, modifies, or deletes, with the diff. root must be a scratch copy of the
// repository's declared inputs that the check may change: the caller owns it.
//
// The comparison is git's, over a repository that exists only for this run and
// lives outside root, so generators see no history, and only the repository's
// own .gitignore files decide which files count: a generator that writes an
// ignored file, such as a build output, changes nothing the check compares.
// A module without any //go:generate directive is an error, so the check
// cannot pass by generating nothing.
func Generate(ctx context.Context, root, module string, env []string) (Report, error) {
	dir := filepath.Join(root, filepath.FromSlash(module))
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return Report{}, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	packages, err := listPackages(ctx, dir, env)
	if err != nil {
		return Report{}, err
	}
	directives, err := countDirectives(packages)
	if err != nil {
		return Report{}, err
	}
	if directives == 0 {
		return Report{}, fmt.Errorf("module %q has no //go:generate directives; refusing an empty pass", module)
	}

	gitDir, err := os.MkdirTemp("", "levenshtein-generate-")
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = os.RemoveAll(gitDir) }() // Scratch repository cleanup.
	repo := scratchRepo{root: root, env: gitEnv(env, gitDir, root)}
	if _, err := repo.git(ctx, "init", "--quiet"); err != nil {
		return Report{}, err
	}
	before, err := repo.tree(ctx)
	if err != nil {
		return Report{}, err
	}

	generated, err := command(ctx, dir, env, "go", "generate", "./...")
	if err != nil {
		return Report{}, err
	}
	if generated.ExitCode != 0 {
		message := fmt.Sprintf("go generate ./... in %s exited %d: %s", module, generated.ExitCode, generated.output())
		if strings.Contains(generated.output(), missingTool) {
			message += "\n" + generateHint
		}
		return Report{}, fmt.Errorf("%s", message)
	}

	after, err := repo.tree(ctx)
	if err != nil {
		return Report{}, err
	}
	findings, err := repo.changes(ctx, before, after, module)
	if err != nil {
		return Report{}, err
	}

	notes := []string{fmt.Sprintf("go generate ./... in %s ran %s and changed %s", module, count(directives, "directive"), count(len(findings), "file"))}
	if output := generated.output(); output != "" {
		notes = append(notes, "go generate output:\n"+truncate(output, maxNoteBytes))
	}
	return Report{Findings: findings, Notes: notes}, nil
}

// countDirectives counts the lines go generate would run, reading the files it
// reads: every Go file of every package, tests included, for this platform.
// Like go generate, it recognizes a directive only at the start of a line.
func countDirectives(packages []listedPackage) (int, error) {
	count := 0
	for _, pkg := range packages {
		for _, name := range pkg.files(true) {
			data, err := os.ReadFile(filepath.Join(pkg.Dir, name))
			if err != nil {
				return 0, err
			}
			scanner := bufio.NewScanner(bytes.NewReader(data))
			scanner.Buffer(make([]byte, 0, 64<<10), len(data)+1)
			for scanner.Scan() {
				line := scanner.Bytes()
				if rest, ok := bytes.CutPrefix(line, []byte(directiveText)); ok && len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
					count++
				}
			}
			if err := scanner.Err(); err != nil {
				return 0, fmt.Errorf("reading %s: %w", name, err)
			}
		}
	}
	return count, nil
}

// gitEnv runs git against the scratch repository alone: no GIT_* variable from
// the caller can redirect it, and neither system nor global configuration
// applies, so a personal excludes file or diff driver cannot change the
// verdict.
func gitEnv(env []string, gitDir, root string) []string {
	var out []string
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GIT_") {
			out = append(out, entry)
		}
	}
	return append(out, "GIT_DIR="+gitDir, "GIT_WORK_TREE="+root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
}

// scratchRepo is the throwaway repository that records the tree before and
// after go generate.
type scratchRepo struct {
	root string
	env  []string
}

func (r scratchRepo) git(ctx context.Context, args ...string) (string, error) {
	pinned := []string{"-c", "core.excludesFile=", "-c", "core.autocrlf=false", "-c", "core.quotePath=false"}
	ran, err := command(ctx, r.root, r.env, "git", append(pinned, args...)...)
	if err != nil {
		return "", err
	}
	if ran.ExitCode != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", args[0], ran.ExitCode, ran.output())
	}
	return ran.Stdout, nil
}

// tree stages the work tree, honoring its .gitignore files, and returns the
// tree object that records it.
func (r scratchRepo) tree(ctx context.Context) (string, error) {
	if _, err := r.git(ctx, "add", "--all", "--", "."); err != nil {
		return "", err
	}
	tree, err := r.git(ctx, "write-tree")
	return strings.TrimSpace(tree), err
}

// changes reports each file that differs between two trees as one finding,
// located at the first line go generate changed.
func (r scratchRepo) changes(ctx context.Context, before, after, module string) ([]Finding, error) {
	listed, err := r.git(ctx, "diff-tree", "-r", "-z", "--no-renames", "--name-status", before, after)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(listed, "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil, nil
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git diff-tree printed an unexpected listing: %q", listed)
	}

	var findings []Finding
	for i := 0; i < len(fields); i += 2 {
		status, file := gitStatus(fields[i]), fields[i+1]
		diff, err := r.git(ctx, "diff-tree", "-p", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--unified=3", before, after, "--", ":(literal)"+file)
		if err != nil {
			return nil, err
		}
		finding, err := generateFinding(status, file, module, diff)
		if err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

// hunkStart reads the old side of a unified diff's first hunk header.
var hunkStart = regexp.MustCompile(`(?m)^@@ -(\d+)`)

// generateFinding describes one file go generate changed. A modified file is
// located at the first line that differs; a file go generate created or
// deleted at its first line.
func generateFinding(status gitStatus, file, module, diff string) (Finding, error) {
	command := fmt.Sprintf("run go generate ./... in %s and commit the result", module)
	var message string
	line := 1
	switch status {
	case gitAdded:
		message = "go generate ./... creates this file, which the repository does not have; " + command
	case gitDeleted:
		message = "go generate ./... deletes this file; " + command
	case gitModified, gitType:
		message = "go generate ./... changes this file; " + command
		if match := hunkStart.FindStringSubmatch(diff); match != nil {
			if start, err := strconv.Atoi(match[1]); err == nil && start > 0 {
				line = start
			}
		}
	default:
		return Finding{}, fmt.Errorf("git reported status %q for %s", status, file)
	}
	return Finding{
		Code:     CodeGenerate,
		Message:  message + "\n" + capDiff(diff),
		Location: Location{File: path.Clean(file), Line: line},
	}, nil
}

// capDiff keeps a diff's first lines and says how much was left out.
func capDiff(diff string) string {
	lines := strings.SplitAfter(strings.TrimRight(diff, "\n"), "\n")
	var kept strings.Builder
	for i, line := range lines {
		if i == maxDiffLines || kept.Len()+len(line) > maxDiffBytes {
			fmt.Fprintf(&kept, "\n[diff truncated: %d more lines]", len(lines)-i)
			break
		}
		kept.WriteString(line)
	}
	return kept.String()
}

// truncate keeps the start of a long text.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n[truncated]"
}
