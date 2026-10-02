package checktool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// SourceSkipDirs are the directories shell-lint and deps-vuln never enter,
// whatever the target declares: fixtures kept deliberately broken or
// vulnerable, as the go command skips testdata, and third-party code the
// repository does not maintain. secrets scans them, since a credential is
// exposed wherever it is committed.
var SourceSkipDirs = []string{"testdata", "vendor", "node_modules"}

// ShellHeadLimit is how much of an extensionless file shell-lint reads to find
// a shebang.
const ShellHeadLimit = 256

// shellShebang is a first line that runs sh, bash, dash or ksh, directly or
// through env.
var shellShebang = regexp.MustCompile(`^#!\s*/(\S*/)?(env\s+(-S\s+)?)?(sh|bash|dash|ksh)(\s.*)?$`)

// shellExtensions are the file extensions shell-lint always checks.
var shellExtensions = []string{".sh", ".bash"}

// ShellRCNames are the configuration files ShellCheck discovers, which
// shell-lint honors only at the repository root.
var ShellRCNames = []string{".shellcheckrc", "shellcheckrc"}

// ShellScript reports whether shell-lint checks a file: a .sh or .bash file, or
// a file without an extension whose first line, read from at most the first
// ShellHeadLimit bytes, is a shell shebang. head is those bytes. A NUL byte is
// read as \x01 on both executors, since the Dagger path passes heads through a
// NUL-separated listing. Its tests load runner/testdata/shell-scripts.json.
func ShellScript(file string, head []byte) bool {
	extension := path.Ext(path.Base(file))
	if slices.Contains(shellExtensions, extension) {
		return true
	}
	if extension != "" {
		return false
	}

	line, _, _ := bytes.Cut(head[:min(len(head), ShellHeadLimit)], []byte("\n"))
	return shellShebang.Match(bytes.ReplaceAll(line, []byte{0}, []byte{1}))
}

// ShellcheckArguments reports warnings and errors: ShellCheck's info and style
// levels are matters of taste a shared gate should not impose. Without a root
// configuration, --norc keeps ShellCheck from reading one outside the
// repository, such as ~/.shellcheckrc; with one, --rcfile names it, so a nested
// file never applies either.
func ShellcheckArguments(binary, config string, scripts []string) []string {
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

// ShellFindings turns one ShellCheck run into findings, one per diagnostic,
// coded by its SC number. ShellCheck exits 0 without diagnostics and 1 with
// them; 2 means a file could not be read, 3 bad syntax on the command line and
// 4 an unrecognized option, and none of those is ever a pass. An exit that
// contradicts the diagnostics is an error too.
func ShellFindings(run Run) ([]Finding, error) {
	if run.ExitCode != 0 && run.ExitCode != 1 {
		return nil, fmt.Errorf("shellcheck exited %d: %s", run.ExitCode, strings.TrimSpace(run.Stderr+"\n"+run.Stdout))
	}

	var report struct {
		Comments []shellComment `json:"comments"`
	}
	if err := json.Unmarshal([]byte(run.Stdout), &report); err != nil {
		return nil, fmt.Errorf("shellcheck exited %d without its json1 report: %w: %s", run.ExitCode, err, strings.TrimSpace(run.Stderr))
	}
	if report.Comments == nil {
		return nil, fmt.Errorf("shellcheck printed a report without a comments array: %s", output(run))
	}
	if (run.ExitCode == 0) != (len(report.Comments) == 0) {
		return nil, fmt.Errorf("shellcheck exit %d does not match its %d diagnostics: %s", run.ExitCode, len(report.Comments), strings.TrimSpace(run.Stderr))
	}

	findings := make([]Finding, 0, len(report.Comments))
	for _, comment := range report.Comments {
		if comment.File == "" || comment.Line < 1 || comment.Code < 1 || comment.Message == "" {
			return nil, fmt.Errorf("unexpected shellcheck diagnostic: %+v", comment)
		}
		findings = append(findings, Finding{
			Code:     fmt.Sprintf("SC%d", comment.Code),
			Message:  comment.Message,
			Location: Location{File: comment.File, Line: comment.Line, Column: comment.Column},
		})
	}
	return findings, nil
}
