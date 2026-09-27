package verify

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// kindSpec is everything the verifier knows about one check kind: which
// executors run it, how its results are cached, what its findings support,
// where it may run, and whether the no-configuration defaults include it.
// Every decision that depends on the kind reads it through the accessors
// below, so adding a kind is adding one entry to kindSpecs, and
// docs/check-kinds.md is generated from the same entries.
type kindSpec struct {
	kind CheckKind
	// summary is what the kind checks, in one line, for docs/check-kinds.md.
	summary string
	// dagger is the runner function the Dagger executor calls, or "" when
	// only the native executor runs the kind.
	dagger string
	// native is how the native executor validates and runs the kind, or nil
	// when only the Dagger executor runs it. A kind both executors run is a
	// shared Go check.
	native *nativeKind
	// alwaysFresh is why the kind's verdict depends on state no input
	// fingerprint covers, as the report gives it. Its results are never
	// cached, and every Dagger execution gets a nonce.
	alwaysFresh string
	// baseDependent is why the kind's result is not cached, as the report
	// gives it: the CLI reads its input from git history, which no input
	// fingerprint covers. That history travels to Dagger as function
	// arguments, so Dagger can still answer an identical call.
	baseDependent string
	// baseline is whether a baseline file can hold the kind's findings. Each
	// of them names one source location and a message that does not repeat
	// it. go-vet, workflow-lint and the other tool kinds report one finding
	// per module carrying the tool's whole output, line numbers included,
	// which no baseline entry could match across unrelated edits;
	// go-mutation has its own accepted-survivors file.
	baseline bool
	// located is whether the kind's findings name a source location. Every
	// other kind reports one finding per module whose message carries the
	// tool's own output, and its location names the module directory.
	located bool
	// reporting is whether the kind's Dagger function returns a report on a
	// pass.
	reporting bool
	// rootOnly is whether the kind checks the whole repository and so needs a
	// target whose dir is ".".
	rootOnly bool
	// writesWorkspace is whether a check of the kind may write to the source
	// workspace it runs in. Every other kind only reads it.
	writesWorkspace bool
	// runsRepositoryCode is whether the kind's Dagger function runs the
	// repository's own code, which it does with Go cache volumes kept per
	// clone: the executor passes the clone's cache key (see repositoryKey).
	runsRepositoryCode bool
	// goToolchain is whether the kind runs the Go toolchain over the target,
	// natively or in the container. The toolchain reads every source file in a
	// package directory, whatever a //go:embed pattern names below it, and
	// whatever a test opens, including generated code the work tree ignores (a
	// gitignored *.pb.go, a generated SDK), so the kind's file set adds the
	// ignored paths the toolchain can load.
	goToolchain bool
	// defaultCheck is whether the no-configuration defaults declare a check
	// of the kind, with a run of its own named after it.
	defaultCheck bool
	// defaultRuns are the gate runs of the no-configuration defaults that
	// include the kind's check.
	defaultRuns []string
}

// nativeKind is how the native executor runs one check kind. The planner asks
// it to validate configuration and the executor asks it to run; neither needs
// to know which kinds exist.
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

// defaultGates are the gate runs of the no-configuration defaults, in the
// order docs/check-kinds.md lists them. main is also a fresh run.
var defaultGates = []string{"branch", "pre-merge", "main"}

// Reasons the report gives for not reusing a result.
const (
	advisoryReason = "vulnerability scans always query current advisory data"
	moduleReason   = "go mod verify always checks the current module cache"
	mutationReason = "the mutated files depend on the base branch; Dagger reuses identical runs"
	apidiffReason  = "the comparison depends on where the base branch points; Dagger reuses identical runs"
)

// kindSpecs lists every kind, in the order docs/check-kinds.md gives them,
// and checkKinds names them in the same order.
var (
	kindSpecs  []kindSpec
	checkKinds []CheckKind
)

// init fills kindSpecs rather than a variable initializer, because the
// executors it names reach the accessors below again: a command check runs
// stages, whose keys ask whether a kind is a shared Go check.
func init() {
	kindSpecs = describeKinds()
	for _, spec := range kindSpecs {
		checkKinds = append(checkKinds, spec.kind)
	}
}

func describeKinds() []kindSpec {
	return []kindSpec{
		{
			kind:         CheckGoLint,
			goToolchain:  true,
			summary:      "Staticcheck, the curated upstream analyzers and Levenshtein's own LV rules, plus any configured community rule modules on Dagger",
			dagger:       "goLintReport",
			native:       sharedNative((*Native).goLint, "Go policy lint failed"),
			baseline:     true,
			located:      true,
			reporting:    true,
			defaultCheck: true,
			defaultRuns:  defaultGates,
		},
		{
			kind:         CheckGoVet,
			goToolchain:  true,
			summary:      "The pinned Go toolchain's default vet checks",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).goVet, "shared check failed"),
			defaultCheck: true,
			defaultRuns:  defaultGates,
		},
		{
			kind:         CheckGoMod,
			goToolchain:  true,
			summary:      "`go mod tidy -diff` and `go mod verify`: manifests tidy, downloads matching `go.sum`",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).goMod, "shared check failed"),
			alwaysFresh:  moduleReason,
			defaultCheck: true,
			defaultRuns:  defaultGates,
		},
		{
			kind:               CheckGoTest,
			goToolchain:        true,
			summary:            "`go test -race ./...` on the pinned toolchain",
			dagger:             "sharedCheck",
			native:             sharedNative((*Native).goTest, "shared check failed"),
			runsRepositoryCode: true,
			defaultCheck:       true,
		},
		{
			kind:         CheckGoHTTP,
			goToolchain:  true,
			summary:      "bodyclose alone: HTTP response bodies are closed",
			dagger:       "sharedCheck",
			baseline:     true,
			located:      true,
			defaultCheck: true,
		},
		{
			kind:         CheckGoSQL,
			goToolchain:  true,
			summary:      "sqlclosecheck alone: database rows and statements are closed",
			dagger:       "sharedCheck",
			baseline:     true,
			located:      true,
			defaultCheck: true,
		},
		{
			kind:         CheckGoVuln,
			goToolchain:  true,
			summary:      "govulncheck: reachable known vulnerabilities in Go dependencies",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).goVuln, "shared check failed"),
			alwaysFresh:  advisoryReason,
			defaultCheck: true,
			defaultRuns:  []string{"main"},
		},
		{
			kind:         CheckWorkflowLint,
			summary:      "actionlint: GitHub Actions syntax and expressions",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).workflowLint, "shared check failed"),
			rootOnly:     true,
			defaultCheck: true,
		},
		{
			kind:         CheckWorkflowSecurity,
			summary:      "zizmor's offline audits of workflows, composite actions and Dependabot configuration",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).workflowSecurity, "shared check failed"),
			rootOnly:     true,
			defaultCheck: true,
		},
		{
			kind:         CheckShellLint,
			summary:      "ShellCheck's warnings and errors in the repository's shell scripts",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).shellLint, "shared check failed"),
			baseline:     true,
			located:      true,
			rootOnly:     true,
			defaultCheck: true,
		},
		{
			kind:         CheckSecrets,
			summary:      "gitleaks' default rules over the repository's files, every value redacted",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).secrets, "shared check failed"),
			located:      true,
			rootOnly:     true,
			defaultCheck: true,
		},
		{
			kind:         CheckDepsVuln,
			summary:      "osv-scanner: known vulnerabilities in non-Go dependency lockfiles",
			dagger:       "sharedCheck",
			native:       sharedNative((*Native).depsVuln, "shared check failed"),
			alwaysFresh:  advisoryReason,
			located:      true,
			rootOnly:     true,
			defaultCheck: true,
		},
		{
			kind:    CheckSelfTest,
			summary: "Levenshtein's own good and bad fixtures, for developing the shared checks",
			dagger:  "selfTest",
		},
		{
			kind:    CheckCommand,
			summary: "A command the repository declares, run as a trusted host process",
			native: &nativeKind{
				validate:   validateCommandCheck,
				rerunReady: commandRerunReady,
				execute:    (*Native).runCommand,
			},
			writesWorkspace: true,
		},
		{
			kind:    CheckSemanticLint,
			summary: "Advisory Jev judgments about Go comments, errors, tests, docs and PR shape",
			native: &nativeKind{
				validate:   validateSemanticLint,
				rerunReady: alwaysReady,
				execute: func(_ *Native, ctx context.Context, req Request, dir string, env []string) Result {
					return semanticLint(ctx, req, dir, env)
				},
			},
		},
		{
			kind:               CheckGoMutation,
			goToolchain:        true,
			summary:            "gremlins mutation testing of the Go files a branch changed",
			dagger:             "goMutation",
			baseDependent:      mutationReason,
			located:            true,
			reporting:          true,
			runsRepositoryCode: true,
		},
		{
			kind:        CheckGoImports,
			goToolchain: true,
			summary:     "The repository's layering rules: which of its packages may import which",
			dagger:      "goImports",
			native:      sharedNative((*Native).goImports, "shared check failed"),
			baseline:    true,
			located:     true,
		},
		{
			kind:               CheckGoGenerate,
			goToolchain:        true,
			summary:            "`go generate ./...` in a scratch copy: every file it would add, change or delete",
			dagger:             "goGenerate",
			native:             sharedNative((*Native).goGenerate, "shared check failed"),
			located:            true,
			runsRepositoryCode: true,
			defaultCheck:       true,
		},
		{
			kind:        CheckGoApidiff,
			goToolchain: true,
			summary:     "apidiff between the branch's merge base and the working tree: incompatible exported API changes",
			dagger:      "goApidiff",
			native: &nativeKind{
				validate:   validateSharedGoCheck,
				rerunReady: alwaysReady,
				execute:    (*Native).apidiff,
			},
			baseDependent: apidiffReason,
			located:       true,
			defaultCheck:  true,
		},
	}
}

// sharedNative is the native side of a shared Go kind that reports its
// findings through goCheckExecutor.
func sharedNative(run goRunner, message string) *nativeKind {
	return &nativeKind{validate: validateSharedGoCheck, rerunReady: alwaysReady, execute: goCheckExecutor(run, message)}
}

// specOf is a kind's descriptor, or the zero descriptor, which supports
// nothing, for a kind that does not exist.
func specOf(kind CheckKind) kindSpec {
	for _, spec := range kindSpecs {
		if spec.kind == kind {
			return spec
		}
	}
	return kindSpec{}
}

// daggerFunction is the runner function that runs a kind on Dagger, or "".
func daggerFunction(kind CheckKind) string {
	return specOf(kind).dagger
}

// nativeKindOf is how the native executor runs a kind, if it does.
func nativeKindOf(kind CheckKind) (nativeKind, bool) {
	native := specOf(kind).native
	if native == nil {
		return nativeKind{}, false
	}
	return *native, true
}

// sharedGoCheck reports whether either executor can run a kind. Its results
// are cacheable by kind, the way a Dagger check's are, because it carries no
// command object to opt in with; the always-fresh kinds are still never
// cached, whichever executor runs them.
func sharedGoCheck(kind CheckKind) bool {
	spec := specOf(kind)
	return spec.dagger != "" && spec.native != nil
}

// alwaysFreshReason is why a kind's results are never reused, or "".
func alwaysFreshReason(kind CheckKind) string {
	return specOf(kind).alwaysFresh
}

// baseDependentReason is why the CLI does not cache a kind that reads git
// history, or "".
func baseDependentReason(kind CheckKind) string {
	return specOf(kind).baseDependent
}

// baselineKind reports whether a baseline file can hold a kind's findings.
func baselineKind(kind CheckKind) bool {
	return specOf(kind).baseline
}

// locatedKind reports whether a kind's findings name a source location.
func locatedKind(kind CheckKind) bool {
	return specOf(kind).located
}

// reportsOnPass reports whether a kind's Dagger function returns a report
// when the check passes.
func reportsOnPass(kind CheckKind) bool {
	return specOf(kind).reporting
}

// readsGoToolchain reports whether a kind runs the Go toolchain over its target.
func readsGoToolchain(kind CheckKind) bool {
	return specOf(kind).goToolchain
}

// runsRepositoryCode reports whether a kind's Dagger function runs the
// repository's own code, and so takes the clone's cache key.
func runsRepositoryCode(kind CheckKind) bool {
	return specOf(kind).runsRepositoryCode
}

// rootOnly reports whether a kind needs a repository-root target.
func rootOnly(kind CheckKind) bool {
	return specOf(kind).rootOnly
}

// readOnlyWorkspace reports whether a check of the kind only reads the source
// workspace it runs in, so it can share the workspace with other such checks.
// A command check may write anything there. A check's preparation and build
// stages write their outputs whatever its kind.
func readOnlyWorkspace(kind CheckKind) bool {
	return !specOf(kind).writesWorkspace
}

// A kind that always executes verification needs nothing declared for a fresh
// run: the shared Go kinds bypass their own analysis caches themselves, and
// semantic-lint has no verdict cache at all.
func alwaysReady(Check) error { return nil }

// kindNames lists the kinds a predicate holds for, sorted and joined.
func kindNames(holds func(kindSpec) bool) string {
	var names []string
	for _, spec := range kindSpecs {
		if holds(spec) {
			names = append(names, string(spec.kind))
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func nativeKindNames() string {
	return kindNames(func(spec kindSpec) bool { return spec.native != nil })
}

func baselineKindNames() string {
	return kindNames(func(spec kindSpec) bool { return spec.baseline })
}

// A fresh run must execute verification, so a command check has to say what
// bypasses its own verdict cache.
func commandRerunReady(check Check) error {
	if check.Command == nil || len(check.Command.RerunArgs) == 0 {
		return fmt.Errorf("fresh native runs require explicit rerun_args")
	}
	return nil
}
