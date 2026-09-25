package main

import (
	"context"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"
)

// shellListing prints every file outside the skipped directories as its path
// and, for a file without an extension that starts with #!, the first line of
// its first ShellHeadLimit bytes, each followed by a NUL. NULs in that line
// become \x01 so they cannot split the listing. That line is all ShellScript
// reads, and the listing is an exec's stdout, which Dagger streams to its
// progress log and traces, so no other contents, such as the start of an
// extensionless key file or a script's body, are printed.
func shellListing() []string {
	var prune []string
	for i, dir := range checktool.SourceSkipDirs {
		if i > 0 {
			prune = append(prune, "-o")
		}
		prune = append(prune, "-name", dir)
	}
	script := fmt.Sprintf(`for f do printf '%%s\0' "$f"; case "${f##*/}" in *.*) ;; *) line=$(head -c %d "$f" | tr '\000' '\001' | head -n 1); case $line in '#!'*) printf '%%s' "$line" ;; esac ;; esac; printf '\0'; done`, checktool.ShellHeadLimit)
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
		if slices.ContainsFunc(strings.Split(path.Dir(file), "/"), func(dir string) bool { return slices.Contains(checktool.SourceSkipDirs, dir) }) {
			continue
		}
		if slices.Contains(checktool.ShellRCNames, file) {
			configs = append(configs, file)
		}
		if checktool.ShellScript(file, []byte(fields[i+1])) {
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

	run, _, err := runTool(ctx, ctr, checktool.ShellcheckArguments("/usr/local/bin/shellcheck", config, scripts))
	if err != nil {
		return nil, err
	}
	return checktool.ShellFindings(run)
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
