package checktool

import (
	"fmt"
	"regexp"
	"strings"
)

// ModStep is one of the two go commands a go-mod check runs, in order: whether
// go.mod and go.sum are already what tidy would write, then whether the
// downloaded dependencies still match the hashes go.sum recorded.
type ModStep string

const (
	ModTidy   ModStep = "tidy -diff"
	ModVerify ModStep = "verify"
)

// ModSteps are the steps in the order a go-mod check runs them.
var ModSteps = []ModStep{ModTidy, ModVerify}

// Args is the step's go command.
func (s ModStep) Args() []string {
	return append([]string{"go", "mod"}, strings.Fields(string(s))...)
}

// modifiedModule is how go mod verify names a download that no longer matches
// the hash recorded when it was fetched.
var modifiedModule = regexp.MustCompile(`(?m)^\S+ \S+: (zip has been modified|dir has been modified|missing ziphash)`)

// ModFindings tells a go-mod step's diagnostics from a tool error. Both
// commands exit 1 either way, so the output decides: tidy -diff prints a diff
// on stdout only when the manifests are untidy, verify names each module whose
// download was modified, and either reports a SECURITY ERROR when a download
// disagrees with go.sum. Anything else, such as an unreachable module proxy, is
// an error and never a pass. The go command's own output is the finding.
func ModFindings(step ModStep, module string, run Run) ([]Finding, error) {
	if run.ExitCode == 0 {
		return nil, nil
	}

	mismatch := strings.Contains(run.Stderr, "SECURITY ERROR")
	switch step {
	case ModTidy:
		mismatch = mismatch || strings.TrimSpace(run.Stdout) != ""
	case ModVerify:
		mismatch = mismatch || modifiedModule.MatchString(run.Stderr)
	}
	if run.ExitCode != 1 || !mismatch {
		return nil, fmt.Errorf("go mod %s exited %d: %s", step, run.ExitCode, output(run))
	}
	return whole(KindGoMod, module, output(run)), nil
}

// ModTampered reports whether a go-mod finding's message says a download
// disagrees with go.sum or changed after it was fetched, which tidying the
// manifests cannot fix.
func ModTampered(message string) bool {
	return strings.Contains(message, "SECURITY ERROR") || modifiedModule.MatchString(message)
}
