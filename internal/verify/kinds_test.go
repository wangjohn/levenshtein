package verify

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// Every kind is owned by exactly one executor, so planning can never accept a
// check that no executor knows how to run, and every native entry is complete,
// so the planner and executor can call its functions without nil checks.
func TestEveryCheckKindHasExactlyOneCompleteExecutor(t *testing.T) {
	for _, kind := range checkKinds {
		dagger := daggerFunctions[kind] != "" // Planning tests the value, not key presence.
		native, ok := nativeKinds[kind]
		if dagger == ok {
			t.Errorf("%s must be owned by exactly one executor (dagger=%v native=%v)", kind, dagger, ok)
		}
		if ok && (native.validate == nil || native.rerunReady == nil || native.execute == nil) {
			t.Errorf("%s: native kind entries need validate, rerunReady, and execute", kind)
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
	req.Check = Check{Kind: CheckGoLint}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "command, semantic-lint") {
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
