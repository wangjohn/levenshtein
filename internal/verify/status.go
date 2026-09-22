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

type CheckKind string

const (
	CheckGoLint       CheckKind = "go-lint"
	CheckGoVet        CheckKind = "go-vet"
	CheckGoHTTP       CheckKind = "go-http"
	CheckGoSQL        CheckKind = "go-sql"
	CheckGoVuln       CheckKind = "go-vuln"
	CheckWorkflowLint CheckKind = "workflow-lint"
	CheckSelfTest     CheckKind = "self-test"
	CheckCommand      CheckKind = "command"
	CheckSemanticLint CheckKind = "semantic-lint"
)

// checkKinds lists every kind, so tests can prove each one has exactly one
// executor. Add new kinds here as well as to the executor that runs them.
var checkKinds = []CheckKind{CheckGoLint, CheckGoVet, CheckGoHTTP, CheckGoSQL, CheckGoVuln, CheckWorkflowLint, CheckSelfTest, CheckCommand, CheckSemanticLint}

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
