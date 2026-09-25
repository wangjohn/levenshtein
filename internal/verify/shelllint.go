package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// sourceSkipDirs are the directories shell-lint never enters, whatever the
// target declares: fixtures kept deliberately broken, as the go command skips
// testdata, and third-party code the repository does not maintain. secrets
// scans them, since a credential is exposed wherever it is committed.
// runner/shelllint.go keeps a copy; change both together.
var sourceSkipDirs = []string{"testdata", "vendor", "node_modules"}

func sourceSkipDir(name string) bool {
	return slices.Contains(sourceSkipDirs, name)
}

// shellHeadLimit is how much of an extensionless file shell-lint reads to find
// a shebang.
const shellHeadLimit = 256

// shellShebang is a first line that runs sh, bash, dash or ksh, directly or
// through env. runner/shelllint.go keeps a copy; change both together.
var shellShebang = regexp.MustCompile(`^#!\s*/(\S*/)?(env\s+(-S\s+)?)?(sh|bash|dash|ksh)(\s.*)?$`)

// shellExtensions are the file extensions shell-lint always checks.
var shellExtensions = []string{".sh", ".bash"}

// shellScript reports whether shell-lint checks a file: a .sh or .bash file, or
// a file without an extension whose first line, read from at most the first
// shellHeadLimit bytes, is a shell shebang. head is those bytes. A NUL byte is
// read as \x01 on both executors, since the Dagger path passes heads through a
// NUL-separated listing. runner/shelllint.go keeps a copy; change both together.
func shellScript(file string, head []byte) bool {
	extension := path.Ext(path.Base(file))
	if slices.Contains(shellExtensions, extension) {
		return true
	}
	if extension != "" {
		return false
	}

	line, _, _ := bytes.Cut(head[:min(len(head), shellHeadLimit)], []byte("\n"))
	return shellShebang.Match(bytes.ReplaceAll(line, []byte{0}, []byte{1}))
}

// shellRCNames are the configuration files ShellCheck discovers, which
// shell-lint honors only at the repository root.
var shellRCNames = []string{".shellcheckrc", "shellcheckrc"}

// shellInputs names the scripts shell-lint checks and the root configuration it
// honors, from the files the target makes visible (see visibleFiles).
func shellInputs(source string, files []string) ([]string, string, error) {
	var scripts, configs []string
	for _, file := range files {
		if slices.Contains(shellRCNames, file) {
			configs = append(configs, file)
		}

		var head []byte
		if path.Ext(path.Base(file)) == "" {
			read, err := readHead(filepath.Join(source, filepath.FromSlash(file)), shellHeadLimit)
			if err != nil {
				return nil, "", err
			}
			head = read
		}
		if shellScript(file, head) {
			scripts = append(scripts, file)
		}
	}

	if len(scripts) == 0 {
		return nil, "", fmt.Errorf("shell-lint found no shell scripts (*.sh, *.bash, or an extensionless file with a sh, bash, dash or ksh shebang) to check")
	}
	if len(configs) > 1 {
		return nil, "", fmt.Errorf("configure only one ShellCheck file; found %s", strings.Join(configs, ", "))
	}
	config := ""
	if len(configs) == 1 {
		config = configs[0]
	}
	return scripts, config, nil
}

func readHead(file string, limit int) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // Read-only.
	return io.ReadAll(io.LimitReader(f, int64(limit)))
}

func (n *Native) shellLint(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	files, err := visibleFiles(req.Source, req.Target.Inputs, req.Target.Exclude, sourceSkipDir)
	if err != nil {
		return nil, toolRun{}, err
	}
	scripts, config, err := shellInputs(req.Source, files)
	if err != nil {
		return nil, toolRun{}, err
	}
	binary, err := installRelease(ctx, req.Shared, work.Root, releaseShellCheck, "")
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, req.Source, shellcheckArguments(binary, config, scripts), shellEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := shellFindings(run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

// shellEnv drops SHELLCHECK_OPTS, which would otherwise add the host's own
// flags to the check's. The container has none.
func shellEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		return strings.HasPrefix(entry, "SHELLCHECK_OPTS=")
	})
}

// shellcheckArguments reports warnings and errors: ShellCheck's info and style
// levels are matters of taste a shared gate should not impose. Without a root
// configuration, --norc keeps ShellCheck from reading one outside the
// repository, such as ~/.shellcheckrc; with one, --rcfile names it, so a nested
// file never applies either. runner/shelllint.go keeps a copy; change both
// together.
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
// contradicts the diagnostics is an error too. runner/shelllint.go keeps a
// copy; change both together.
func shellFindings(exitCode int, stdout, stderr string) ([]finding, error) {
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

	findings := make([]finding, 0, len(report.Comments))
	for _, comment := range report.Comments {
		if comment.File == "" || comment.Line < 1 || comment.Code < 1 || comment.Message == "" {
			return nil, fmt.Errorf("unexpected shellcheck diagnostic: %+v", comment)
		}
		findings = append(findings, finding{
			Code:     fmt.Sprintf("SC%d", comment.Code),
			Message:  comment.Message,
			Location: location{File: comment.File, Line: comment.Line, Column: comment.Column},
		})
	}
	return findings, nil
}
