package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"dagger/levenshtein/internal/dagger"
)

// sourceSkipDirs are the directories shell-lint never enters: fixtures kept
// deliberately broken, as the go command skips testdata, and third-party code
// the repository does not maintain. internal/verify/shelllint.go keeps a copy;
// change both together.
var sourceSkipDirs = []string{"testdata", "vendor", "node_modules"}

// shellHeadLimit is how much of an extensionless file shell-lint reads to find
// a shebang.
const shellHeadLimit = 256

// shellShebang is a first line that runs sh, bash, dash or ksh, directly or
// through env. internal/verify/shelllint.go keeps a copy; change both together.
var shellShebang = regexp.MustCompile(`^#!\s*/(\S*/)?(env\s+(-S\s+)?)?(sh|bash|dash|ksh)(\s.*)?$`)

// shellExtensions are the file extensions shell-lint always checks.
var shellExtensions = []string{".sh", ".bash"}

// shellRCNames are the configuration files ShellCheck discovers, which
// shell-lint honors only at the repository root.
var shellRCNames = []string{".shellcheckrc", "shellcheckrc"}

// shellScript reports whether shell-lint checks a file: a .sh or .bash file, or
// a file without an extension whose first line, read from at most the first
// shellHeadLimit bytes, is a shell shebang. A NUL byte reads as \x01, since the
// listing below is NUL-separated. internal/verify/shelllint.go keeps a copy;
// change both together.
func shellScript(file string, head []byte) bool {
	extension := path.Ext(path.Base(file))
	if slices.Contains(shellExtensions, extension) {
		return true
	}
	if extension != "" {
		return false
	}

	if len(head) > shellHeadLimit {
		head = head[:shellHeadLimit]
	}
	line, _, _ := bytes.Cut(head, []byte("\n"))
	return shellShebang.Match(bytes.ReplaceAll(line, []byte{0}, []byte{1}))
}

// shellListing prints every file outside the skipped directories as its path
// and, for a file without an extension, the start of its contents, each
// followed by a NUL. NULs in the contents become \x01 so they cannot split
// the listing.
func shellListing() []string {
	var prune []string
	for i, dir := range sourceSkipDirs {
		if i > 0 {
			prune = append(prune, "-o")
		}
		prune = append(prune, "-name", dir)
	}
	script := fmt.Sprintf(`for f do printf '%%s\0' "$f"; case "${f##*/}" in *.*) ;; *) head -c %d "$f" | tr '\000' '\001' ;; esac; printf '\0'; done`, shellHeadLimit)
	args := append([]string{"find", ".", "-type", "d", "("}, prune...)
	return append(args, ")", "-prune", "-o", "-type", "f", "-exec", "sh", "-c", script, "sh", "{}", "+")
}

// shellInputs names the scripts shell-lint checks and the root configuration
// it honors, from shellListing's output.
func shellInputs(listing string) ([]string, string, error) {
	fields := strings.Split(listing, "\x00")
	if len(fields)%2 != 1 || fields[len(fields)-1] != "" {
		return nil, "", fmt.Errorf("unexpected file listing for shell-lint")
	}

	var scripts, configs []string
	for i := 0; i+1 < len(fields); i += 2 {
		file := strings.TrimPrefix(fields[i], "./")
		if slices.ContainsFunc(strings.Split(path.Dir(file), "/"), func(dir string) bool { return slices.Contains(sourceSkipDirs, dir) }) {
			continue
		}
		if slices.Contains(shellRCNames, file) {
			configs = append(configs, file)
		}
		if shellScript(file, []byte(fields[i+1])) {
			scripts = append(scripts, file)
		}
	}

	if len(scripts) == 0 {
		return nil, "", fmt.Errorf("shell-lint found no shell scripts (*.sh, *.bash, or an extensionless file with a sh, bash, dash or ksh shebang) to check")
	}
	if len(configs) > 1 {
		sort.Strings(configs)
		return nil, "", fmt.Errorf("configure only one ShellCheck file; found %s", strings.Join(configs, ", "))
	}
	sort.Strings(scripts)
	config := ""
	if len(configs) == 1 {
		config = configs[0]
	}
	return scripts, config, nil
}

// shellLint checks the repository's shell scripts with the pinned ShellCheck,
// installed before the source is added so the download is shared across
// sources and runs.
func shellLint(ctx context.Context, source *dagger.Directory, tools toolchain, nonce string) ([]diagnostic, error) {
	ctr, err := withRelease(ctx, dag.Container().From(tools.GoImage), tools.ShellCheck, "shellcheck", "/usr/local/bin/shellcheck")
	if err != nil {
		return nil, err
	}
	ctr = ctr.WithDirectory("/src", source).WithWorkdir("/src")
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	listing, err := ctr.WithExec(shellListing()).Stdout(ctx)
	if err != nil {
		return nil, err
	}
	scripts, config, err := shellInputs(listing)
	if err != nil {
		return nil, err
	}

	checked := ctr.WithExec(shellcheckArguments("/usr/local/bin/shellcheck", config, scripts), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return nil, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	return shellFindings(exitCode, stdout, stderr)
}

// shellcheckArguments reports warnings and errors: ShellCheck's info and style
// levels are matters of taste a shared gate should not impose. Without a root
// configuration, --norc keeps ShellCheck from reading one elsewhere; with one,
// --rcfile names it, so a nested file never applies either.
// internal/verify/shelllint.go keeps a copy; change both together.
func shellcheckArguments(binary, config string, scripts []string) []string {
	args := []string{binary, "--format=json1", "--severity=warning", "--color=never"}
	if config == "" {
		args = append(args, "--norc")
	} else {
		args = append(args, "--rcfile="+config)
	}
	args = append(args, "--")
	return append(args, scripts...)
}

// shellComment is one diagnostic in ShellCheck's json1 format. The tags are
// ShellCheck's field names.
type shellComment struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// shellFindings turns one ShellCheck run into findings, one per diagnostic,
// coded by its SC number. ShellCheck exits 0 without diagnostics and 1 with
// them; 2 means a file could not be read, 3 bad syntax on the command line and
// 4 an unrecognized option, and none of those is ever a pass. An exit that
// contradicts the diagnostics is an error too. internal/verify/shelllint.go
// keeps a copy; change both together.
func shellFindings(exitCode int, stdout, stderr string) ([]diagnostic, error) {
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("shellcheck exited %d: %s", exitCode, strings.TrimSpace(stderr+"\n"+stdout))
	}

	var report struct {
		Comments []shellComment `json:"comments"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		return nil, fmt.Errorf("shellcheck exited %d without its json1 report: %w: %s", exitCode, err, strings.TrimSpace(stderr))
	}
	if (exitCode == 0) != (len(report.Comments) == 0) {
		return nil, fmt.Errorf("shellcheck exit %d does not match its %d diagnostics: %s", exitCode, len(report.Comments), strings.TrimSpace(stderr))
	}

	findings := make([]diagnostic, 0, len(report.Comments))
	for _, comment := range report.Comments {
		if comment.File == "" || comment.Line < 1 || comment.Code < 1 || comment.Message == "" {
			return nil, fmt.Errorf("unexpected shellcheck diagnostic: %+v", comment)
		}
		findings = append(findings, diagnostic{
			Code:     fmt.Sprintf("SC%d", comment.Code),
			Message:  comment.Message,
			Location: location{File: comment.File, Line: comment.Line, Column: comment.Column},
		})
	}
	return findings, nil
}

// shellLintSelfTest proves the pinned ShellCheck downloads, verifies and runs:
// shell-good passes only because its root .shellcheckrc is honored, and its
// info- and style-level issues and its testdata script stay unreported;
// shell-bad reports each warning and error at its location, including in an
// extensionless script found by its shebang and a .bash file without one.
func shellLintSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	good, err := shellLint(ctx, fixtures.Directory("shell-good"), tools, nonce)
	if err != nil || len(good) != 0 {
		return fmt.Errorf("shell-good fixture must pass shell-lint: findings=%v error=%v", good, err)
	}

	bad, err := shellLint(ctx, fixtures.Directory("shell-bad"), tools, nonce)
	if err != nil {
		return fmt.Errorf("shell-bad fixture must fail for its diagnostics, not a tool error: %w", err)
	}
	var got []string
	for _, finding := range bad {
		got = append(got, fmt.Sprintf("%s %s:%d:%d", finding.Code, finding.Location.File, finding.Location.Line, finding.Location.Column))
	}
	sort.Strings(got)
	if !slices.Equal(got, shellBadFindings) {
		return fmt.Errorf("shell-bad fixture must report exactly %v; got %v", shellBadFindings, got)
	}
	return nil
}

// shellBadFindings are the diagnostics shell-bad produces, sorted.
// internal/verify's integration test expects the same list.
var shellBadFindings = []string{"SC2034 run.sh:3:1", "SC2164 run.sh:2:1", "SC2168 lib.bash:1:1", "SC3010 bin/tool:2:4"}
