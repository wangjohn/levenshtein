package verify

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// nativeKind is one check kind the native executor runs. The planner asks it
// to validate configuration and the executor asks it to run; neither needs to
// know which kinds exist. Adding a kind is adding a map entry.
type nativeKind struct {
	// validate accepts a check and its environment or explains what is wrong.
	// It also rejects option objects that belong to another kind.
	validate func(Check, Environment) error
	// rerunReady reports whether a fresh run can execute this check. Kinds that
	// always execute return nil.
	rerunReady func(Check) error
	// execute runs the check in dir with the resolved environment. Kinds that
	// need no executor state ignore the receiver.
	execute func(*Native, context.Context, Request, string, []string) Result
}

var nativeKinds = map[CheckKind]nativeKind{
	CheckCommand: {
		validate:   validateCommandCheck,
		rerunReady: commandRerunReady,
		execute:    (*Native).runCommand,
	},
	CheckSemanticLint: {
		validate:   validateSemanticLint,
		rerunReady: alwaysReady,
		execute: func(_ *Native, ctx context.Context, req Request, dir string, env []string) Result {
			return semanticLint(ctx, req, dir, env)
		},
	},
	CheckGoLint: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goLint, "Go policy lint failed"),
	},
	CheckGoVet: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goVet, "shared check failed"),
	},
	CheckGoMod: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goMod, "shared check failed"),
	},
	CheckGoTest: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goTest, "shared check failed"),
	},
	CheckGoVuln: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goVuln, "shared check failed"),
	},
	CheckWorkflowLint: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).workflowLint, "shared check failed"),
	},
	CheckWorkflowSecurity: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).workflowSecurity, "shared check failed"),
	},
	CheckGoImports: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goImports, "shared check failed"),
	},
	CheckGoGenerate: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).goGenerate, "shared check failed"),
	},
	CheckGoApidiff: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    (*Native).apidiff,
	},
	CheckShellLint: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).shellLint, "shared check failed"),
	},
	CheckSecrets: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).secrets, "shared check failed"),
	},
	CheckDepsVuln: {
		validate:   validateSharedGoCheck,
		rerunReady: alwaysReady,
		execute:    goCheckExecutor((*Native).depsVuln, "shared check failed"),
	},
}

// sharedGoChecks are the kinds either executor can run. Their results are
// cacheable by kind, the way a Dagger check's are, because they carry no
// command object to opt in with; the alwaysFresh kinds are still never cached,
// whichever executor runs them.
var sharedGoChecks = map[CheckKind]bool{
	CheckGoLint:           true,
	CheckGoVet:            true,
	CheckGoMod:            true,
	CheckGoTest:           true,
	CheckGoVuln:           true,
	CheckWorkflowLint:     true,
	CheckWorkflowSecurity: true,
	CheckGoImports:        true,
	CheckGoGenerate:       true,
	CheckGoApidiff:        true,
	CheckShellLint:        true,
	CheckSecrets:          true,
	CheckDepsVuln:         true,
}

// alwaysFresh names the kinds whose verdict depends on state no input
// fingerprint covers, with the reason the report gives for not reusing it.
// Their results are never cached and every Dagger execution gets a nonce.
var alwaysFresh = map[CheckKind]string{
	CheckGoVuln:   "vulnerability scans always query current advisory data",
	CheckGoMod:    "go mod verify always checks the current module cache",
	CheckDepsVuln: "vulnerability scans always query current advisory data",
}

// baseDependent names the kinds whose input the CLI reads from git history,
// which no input fingerprint covers, with the reason the report gives for not
// reusing their results. The history the CLI read travels to Dagger as
// function arguments, so Dagger can still answer an identical call.
var baseDependent = map[CheckKind]string{
	CheckGoMutation: "the mutated files depend on the base branch; Dagger reuses identical runs",
	CheckGoApidiff:  "the comparison depends on where the base branch points; Dagger reuses identical runs",
}

// A kind that always executes verification needs nothing declared for a fresh
// run: the shared Go kinds bypass their own analysis caches themselves, and
// semantic-lint has no verdict cache at all.
func alwaysReady(Check) error { return nil }

func nativeKindNames() string {
	names := make([]string, 0, len(nativeKinds))
	for kind := range nativeKinds {
		names = append(names, string(kind))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// A fresh run must execute verification, so a command check has to say what
// bypasses its own verdict cache.
func commandRerunReady(check Check) error {
	if check.Command == nil || len(check.Command.RerunArgs) == 0 {
		return fmt.Errorf("fresh native runs require explicit rerun_args")
	}
	return nil
}
