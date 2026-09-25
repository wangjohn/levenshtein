package verify

import (
	"context"
	"strings"
	"testing"
)

// Every kind is owned by at least one executor, so planning can never accept a
// check that no executor knows how to run, and every native entry is complete,
// so the planner and executor can call its functions without nil checks. The
// shared Go kinds are the only ones both executors own; a repository chooses
// between them with the environment.
func TestEveryCheckKindHasACompleteExecutor(t *testing.T) {
	seen := map[CheckKind]bool{}
	for _, kind := range checkKinds {
		native, ok := nativeKindOf(kind)
		if daggerFunction(kind) == "" && !ok {
			t.Errorf("%s is owned by no executor", kind)
		}
		if ok && (native.validate == nil || native.rerunReady == nil || native.execute == nil) {
			t.Errorf("%s: native kind entries need validate, rerunReady, and execute", kind)
		}
		if specOf(kind).summary == "" {
			t.Errorf("%s needs a summary for docs/check-kinds.md", kind)
		}
		if seen[kind] {
			t.Errorf("%s is described twice", kind)
		}
		seen[kind] = true
	}
}

// Only a command check may write to the workspace it runs in; every other
// kind can share it.
func TestOnlyCommandChecksWriteTheirWorkspace(t *testing.T) {
	for _, kind := range checkKinds {
		if readOnlyWorkspace(kind) == (kind == CheckCommand) {
			t.Errorf("%s: readOnlyWorkspace = %v", kind, readOnlyWorkspace(kind))
		}
	}
}

// sharedGoKinds lists the kinds either executor runs, in kindSpecs order.
func sharedGoKinds() []CheckKind {
	var kinds []CheckKind
	for _, kind := range checkKinds {
		if sharedGoCheck(kind) {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func TestNativeExecutorNamesItsKindsForUnknownCheck(t *testing.T) {
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckSelfTest}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "command, deps-vuln, go-apidiff, go-generate, go-imports, go-lint, go-mod, go-test, go-vet, go-vuln, secrets, semantic-lint, shell-lint, workflow-lint, workflow-security") {
		t.Fatalf("unknown kind: %+v", result)
	}
}

func TestCommandCheckWithoutOptionsIsAnError(t *testing.T) {
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckCommand}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "command options") {
		t.Fatalf("missing options: %+v", result)
	}
}
