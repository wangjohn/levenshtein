package verify

import (
	"context"
	"strings"
	"testing"
)

// Every kind is owned by exactly one executor, so planning can never accept a
// check that no executor knows how to run.
func TestEveryCheckKindHasExactlyOneExecutor(t *testing.T) {
	for _, kind := range []CheckKind{CheckGoLint, CheckGoVet, CheckGoHTTP, CheckGoSQL, CheckGoVuln, CheckWorkflowLint, CheckSelfTest, CheckCommand, CheckSemanticLint} {
		_, dagger := daggerFunctions[kind]
		_, native := nativeKinds[kind]
		if dagger == native {
			t.Errorf("%s must be owned by exactly one executor (dagger=%v native=%v)", kind, dagger, native)
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
