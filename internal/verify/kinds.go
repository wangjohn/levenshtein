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
		rerunReady: func(Check) error { return nil },
		execute: func(_ *Native, ctx context.Context, req Request, dir string, env []string) Result {
			return semanticLint(ctx, req, dir, env)
		},
	},
}

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
