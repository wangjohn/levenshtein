package verify

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// Every kind is owned by at least one executor, so planning can never accept a
// check that no executor knows how to run, and every native entry is complete,
// so the planner and executor can call its functions without nil checks. The
// shared Go kinds are the only ones both executors own; a repository chooses
// between them with the environment.
func TestEveryCheckKindHasACompleteExecutor(t *testing.T) {
	for _, kind := range checkKinds {
		dagger := daggerFunctions[kind] != "" // Planning tests the value, not key presence.
		native, ok := nativeKinds[kind]
		if !dagger && !ok {
			t.Errorf("%s is owned by no executor", kind)
		}
		if dagger && ok != sharedGoChecks[kind] {
			t.Errorf("%s is owned by both executors but is not a shared Go check", kind)
		}
		if ok && (native.validate == nil || native.rerunReady == nil || native.execute == nil) {
			t.Errorf("%s: native kind entries need validate, rerunReady, and execute", kind)
		}
	}
	for kind := range sharedGoChecks {
		if daggerFunctions[kind] == "" || nativeKinds[kind].execute == nil {
			t.Errorf("%s must be runnable by both executors", kind)
		}
	}
	for kind := range nativeKinds {
		if !slices.Contains(checkKinds, kind) {
			t.Errorf("%s is registered but missing from checkKinds", kind)
		}
	}
	for kind := range daggerFunctions {
		if !slices.Contains(checkKinds, kind) {
			t.Errorf("%s is registered but missing from checkKinds", kind)
		}
	}
}

func TestNativeExecutorNamesItsKindsForUnknownCheck(t *testing.T) {
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckSelfTest}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "command, deps-vuln, go-lint, go-mod, go-test, go-vet, go-vuln, secrets, semantic-lint, shell-lint, workflow-lint, workflow-security") {
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
