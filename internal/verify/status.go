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
	CheckGoLint   CheckKind = "go-lint"
	CheckSelfTest CheckKind = "self-test"
	CheckCommand  CheckKind = "command"
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
