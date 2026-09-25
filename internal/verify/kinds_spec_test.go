package verify

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// kindRow is everything the verifier decides about one check kind, as the
// planner, the executors, the cache, the baseline, the renderers and the
// no-configuration defaults each see it.
type kindRow struct {
	Dagger    string
	Native    bool
	Shared    bool
	Fresh     string
	Base      string
	Baseline  bool
	Located   bool
	Reporting bool
	RootOnly  bool
	Default   bool
	Runs      []string
}

// pinnedKinds is what each kind supported when the per-kind tables were
// folded into one descriptor. A change here is a change in behavior, so it
// has to be deliberate.
var pinnedKinds = []struct {
	Kind CheckKind
	Row  kindRow
}{
	{CheckGoLint, kindRow{Dagger: "goLintReport", Native: true, Shared: true, Baseline: true, Located: true, Reporting: true, Default: true, Runs: []string{"branch", "pre-merge", "main"}}},
	{CheckGoVet, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Default: true, Runs: []string{"branch", "pre-merge", "main"}}},
	{CheckGoMod, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Fresh: "go mod verify always checks the current module cache", Default: true, Runs: []string{"branch", "pre-merge", "main"}}},
	{CheckGoTest, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Default: true}},
	{CheckGoHTTP, kindRow{Dagger: "sharedCheck", Baseline: true, Located: true, Default: true}},
	{CheckGoSQL, kindRow{Dagger: "sharedCheck", Baseline: true, Located: true, Default: true}},
	{CheckGoVuln, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Fresh: "vulnerability scans always query current advisory data", Default: true, Runs: []string{"main"}}},
	{CheckWorkflowLint, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, RootOnly: true, Default: true}},
	{CheckWorkflowSecurity, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, RootOnly: true, Default: true}},
	{CheckShellLint, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Baseline: true, Located: true, RootOnly: true, Default: true}},
	{CheckSecrets, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Located: true, RootOnly: true, Default: true}},
	{CheckDepsVuln, kindRow{Dagger: "sharedCheck", Native: true, Shared: true, Fresh: "vulnerability scans always query current advisory data", Located: true, RootOnly: true, Default: true}},
	{CheckSelfTest, kindRow{Dagger: "selfTest"}},
	{CheckCommand, kindRow{Native: true}},
	{CheckSemanticLint, kindRow{Native: true}},
	{CheckGoMutation, kindRow{Dagger: "goMutation", Base: "the mutated files depend on the base branch; Dagger reuses identical runs", Located: true, Reporting: true}},
	{CheckGoImports, kindRow{Dagger: "goImports", Native: true, Shared: true, Baseline: true, Located: true}},
	{CheckGoGenerate, kindRow{Dagger: "goGenerate", Native: true, Shared: true, Located: true, Default: true}},
	{CheckGoApidiff, kindRow{Dagger: "goApidiff", Native: true, Shared: true, Base: "the comparison depends on where the base branch points; Dagger reuses identical runs", Located: true, Default: true}},
}

func TestCheckKindsKeepTheirBehavior(t *testing.T) {
	var kinds []CheckKind
	for _, pinned := range pinnedKinds {
		kinds = append(kinds, pinned.Kind)
	}
	if !slices.Equal(checkKinds, kinds) {
		t.Fatalf("kinds = %v, want %v", checkKinds, kinds)
	}

	for _, pinned := range pinnedKinds {
		t.Run(string(pinned.Kind), func(t *testing.T) {
			got := observedKind(t, pinned.Kind)
			if got.Dagger != pinned.Row.Dagger || got.Native != pinned.Row.Native || got.Shared != pinned.Row.Shared ||
				got.Fresh != pinned.Row.Fresh || got.Base != pinned.Row.Base || got.Baseline != pinned.Row.Baseline ||
				got.Located != pinned.Row.Located || got.Reporting != pinned.Row.Reporting || got.RootOnly != pinned.Row.RootOnly ||
				got.Default != pinned.Row.Default || !slices.Equal(got.Runs, pinned.Row.Runs) {
				t.Fatalf("got %+v\nwant %+v", got, pinned.Row)
			}
		})
	}
}

// observedKind reads one kind's row from the verifier's tables, and its
// root-only rule from planning itself.
func observedKind(t *testing.T, kind CheckKind) kindRow {
	t.Helper()
	cfg := defaultConfig()
	var runs []string
	for _, run := range []string{"branch", "pre-merge", "main"} {
		if slices.Contains(cfg.Runs[run].Checks, string(kind)) {
			runs = append(runs, run)
		}
	}
	_, isDefault := cfg.Checks[string(kind)]
	_, native := nativeKindOf(kind)
	return kindRow{
		Dagger:    daggerFunction(kind),
		Native:    native,
		Shared:    sharedGoCheck(kind),
		Fresh:     alwaysFreshReason(kind),
		Base:      baseDependentReason(kind),
		Baseline:  baselineKind(kind),
		Located:   locatedKind(kind),
		Reporting: reportsOnPass(kind),
		RootOnly:  plansRootOnly(t, kind),
		Default:   isDefault,
		Runs:      runs,
	}
}

// plansRootOnly plans a valid check of the kind over a nested target that
// does not exist: a root-only kind stops at the target, and every other kind
// gets past it to the missing directory.
func plansRootOnly(t *testing.T, kind CheckKind) bool {
	t.Helper()
	environment := "dagger"
	if daggerFunction(kind) == "" {
		environment = "native"
	}
	var imports *ImportsCheck
	if kind == CheckGoImports {
		imports = &ImportsCheck{Rules: []ImportRule{{Packages: []string{"./..."}, Deny: []string{"os"}, Reason: "r"}}}
	}
	var command *CommandCheck
	if kind == CheckCommand {
		command = &CommandCheck{Args: []string{"true"}}
	}
	check := Check{Kind: kind, Target: "nested", Environment: environment, Imports: imports, Command: command}
	cfg := Config{
		Targets:      map[string]Target{"nested": {Dir: "nested", Inputs: []string{"."}}},
		Environments: map[string]Environment{"dagger": {Executor: ExecutorDagger}, "native": {Executor: ExecutorNative}},
	}

	_, err := cfg.planCheck(t.TempDir(), selection{ID: "check", Check: check}, false)
	if err != nil && strings.Contains(err.Error(), "requires a repository-root target") {
		return true
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("planning %s did not reach the target directory: %v", kind, err)
	}
	return false
}
