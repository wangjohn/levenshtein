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
	CheckSelfTest         CheckKind = "self-test"
	CheckCommand          CheckKind = "command"
	CheckSemanticLint     CheckKind = "semantic-lint"
	CheckGoMutation       CheckKind = "go-mutation"
	CheckGoImports        CheckKind = "go-imports"
)

// checkKinds lists every kind, so tests can prove each one has exactly one
// executor. Add new kinds here as well as to the executor that runs them.
var checkKinds = []CheckKind{
	CheckGoLint, CheckGoVet, CheckGoMod, CheckGoTest, CheckGoHTTP, CheckGoSQL, CheckGoVuln, CheckWorkflowLint, CheckWorkflowSecurity, CheckSelfTest, CheckCommand, CheckSemanticLint, CheckGoMutation,
	CheckGoImports,
}

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
