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
	t.Parallel()
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

// docs/configuration.md tells readers that every go-* kind runs the Go
// toolchain, and so covers the ignored files it can load, rather than listing
// the kinds; a new kind has to keep that true or change the page.
func TestGoKindsAreExactlyTheToolchainKinds(t *testing.T) {
	for _, kind := range checkKinds {
		if readsGoToolchain(kind) != strings.HasPrefix(string(kind), "go-") {
			t.Errorf("%s: readsGoToolchain = %v", kind, readsGoToolchain(kind))
		}
	}
}

// Only a command check may write to the workspace it runs in; every other
// kind can share it.
func TestOnlyCommandChecksWriteTheirWorkspace(t *testing.T) {
	t.Parallel()
	for _, kind := range checkKinds {
		if readOnlyWorkspace(kind) == (kind == CheckCommand) {
			t.Errorf("%s: readOnlyWorkspace = %v", kind, readOnlyWorkspace(kind))
		}
	}
}

// Exactly the kinds whose Dagger functions run the repository's own code pass
// the clone's cache key, so each clone's untrusted caches stay its own and
// every other call stays shareable across clones.
func TestOnlyRepositoryCodeKindsPassACacheKey(t *testing.T) {
	t.Parallel()
	want := []CheckKind{CheckGoTest, CheckGoMutation, CheckGoGenerate}

	var got []CheckKind
	for _, kind := range checkKinds {
		if runsRepositoryCode(kind) {
			got = append(got, kind)
		}
	}

	if !slices.Equal(got, want) {
		t.Errorf("kinds passing a cache key = %v, want %v", got, want)
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
	t.Parallel()
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckSelfTest}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "command, deps-vuln, go-apidiff, go-generate, go-imports, go-lint, go-mod, go-test, go-vet, go-vuln, secrets, semantic-lint, shell-lint, workflow-lint, workflow-security") {
		t.Fatalf("unknown kind: %+v", result)
	}
}

func TestCommandCheckWithoutOptionsIsAnError(t *testing.T) {
	t.Parallel()
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckCommand}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "command options") {
		t.Fatalf("missing options: %+v", result)
	}
}
