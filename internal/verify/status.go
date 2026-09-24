package verify

// Status is the outcome of a check or verification run.
type Status string

const (
	StatusPassed     Status = "passed"
	StatusFailed     Status = "failed"
	StatusError      Status = "error"
	StatusCancelled  Status = "cancelled"
	StatusIncomplete Status = "incomplete"
)

type ExecutorKind string

const (
	ExecutorDagger ExecutorKind = "dagger"
	ExecutorNative ExecutorKind = "native"
)

// DiscoveryKind selects how a target's declared inputs are enumerated for
// fingerprinting. Git discovery asks the work tree which files it tracks or
// would add; filesystem discovery walks every path under each input.
type DiscoveryKind string

const (
	DiscoveryGit        DiscoveryKind = "git"
	DiscoveryFilesystem DiscoveryKind = "filesystem"
)

// discoveryKinds lists every mode, so planning can reject anything else.
var discoveryKinds = []DiscoveryKind{DiscoveryGit, DiscoveryFilesystem}

type CheckKind string

const (
	CheckGoLint           CheckKind = "go-lint"
	CheckGoVet            CheckKind = "go-vet"
	CheckGoMod            CheckKind = "go-mod"
	CheckGoTest           CheckKind = "go-test"
	CheckGoHTTP           CheckKind = "go-http"
	CheckGoSQL            CheckKind = "go-sql"
	CheckGoVuln           CheckKind = "go-vuln"
	CheckWorkflowLint     CheckKind = "workflow-lint"
	CheckWorkflowSecurity CheckKind = "workflow-security"
	CheckShellLint        CheckKind = "shell-lint"
	CheckSecrets          CheckKind = "secrets"
	CheckDepsVuln         CheckKind = "deps-vuln"
	CheckSelfTest         CheckKind = "self-test"
	CheckCommand          CheckKind = "command"
	CheckSemanticLint     CheckKind = "semantic-lint"
	CheckGoMutation       CheckKind = "go-mutation"
	CheckGoImports        CheckKind = "go-imports"
	CheckGoGenerate       CheckKind = "go-generate"
	CheckGoApidiff        CheckKind = "go-apidiff"
)

// checkKinds lists every kind, so tests can prove each one has exactly one
// executor. Add new kinds here as well as to the executor that runs them.
var checkKinds = []CheckKind{
	CheckGoLint, CheckGoVet, CheckGoMod, CheckGoTest, CheckGoHTTP, CheckGoSQL, CheckGoVuln, CheckWorkflowLint, CheckWorkflowSecurity, CheckShellLint, CheckSecrets, CheckDepsVuln, CheckSelfTest, CheckCommand, CheckSemanticLint, CheckGoMutation,
	CheckGoImports, CheckGoGenerate, CheckGoApidiff,
}

// WarningKind names a problem a check result reports without failing. The
// community linter reports the rule-* kinds; runner/community keeps copies of
// those.
type WarningKind string

const (
	// WarningRuleModulesSkipped: a native go-lint check ran its core rules
	// only; community rules run on the Dagger executor.
	WarningRuleModulesSkipped WarningKind = "rule-modules-skipped"
	// WarningRuleModuleDeprecated: this release lists the pinned version as
	// deprecated.
	WarningRuleModuleDeprecated WarningKind = "rule-module-deprecated"
	// WarningRuleModuleRetracted: the module's author retracted the pinned
	// version.
	WarningRuleModuleRetracted WarningKind = "rule-module-retracted"
	// WarningRuleRenamed: a pattern, advisory entry, or setting uses a rule's
	// old name.
	WarningRuleRenamed WarningKind = "rule-renamed"
	// WarningRuleDeprecated: a selected rule is deprecated.
	WarningRuleDeprecated WarningKind = "rule-deprecated"
)

// CacheStatus describes reuse without conflating it with verification outcomes.
type CacheStatus string

const (
	CacheDisabled    CacheStatus = "disabled"
	CacheUnavailable CacheStatus = "unavailable"
	CacheMiss        CacheStatus = "miss"
	CacheHit         CacheStatus = "hit"
	CacheFresh       CacheStatus = "fresh"
)

type StageKind string

const (
	StagePreparation StageKind = "preparation"
	StageBuild       StageKind = "build"
)
