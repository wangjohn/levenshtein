package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"
)

type Result struct {
	ID          string          `json:"id"`
	Status      Status          `json:"status"`
	DurationMS  int64           `json:"duration_ms"`
	VerifiedAt  time.Time       `json:"verified_at"`
	Stdout      string          `json:"stdout,omitempty"`
	Stderr      string          `json:"stderr,omitempty"`
	Error       string          `json:"error,omitempty"`
	Cache       CacheInfo       `json:"cache"`
	ExecutionMS int64           `json:"execution_ms"`
	Stages      []StageResult   `json:"stages,omitempty"`
	Details     json.RawMessage `json:"details,omitempty"`
}

type Report struct {
	Version int      `json:"version"`
	Run     string   `json:"run"`
	Status  Status   `json:"status"`
	Plan    Plan     `json:"plan"`
	Results []Result `json:"results"`
}

type Request struct {
	Source      string
	Shared      string
	RerunChecks bool
	PlannedCheck
}

type Executor interface {
	Execute(context.Context, Request) Result
}

// Cap concurrent checks so a shared Dagger session and CI runners stay within
// predictable CPU/memory use. Independent checks still overlap within the cap.
const maxCheckParallelism = 4

// checkParallelism bounds how many checks run at once. A jobs value above zero
// is the caller's explicit choice and replaces the default cap, including
// raising it: the operator knows what the worker can carry.
func checkParallelism(n, jobs int) int {
	if n <= 1 {
		return 1
	}

	limit := runtime.GOMAXPROCS(0)
	if limit > maxCheckParallelism {
		limit = maxCheckParallelism
	}
	if jobs > 0 {
		limit = jobs
	}
	if limit < 1 {
		limit = 1
	}
	if n < limit {
		return n
	}
	return limit
}

// sharedPreparation is true when both checks declare the same preparation stage.
// Those checks run in plan order so the later one can reuse recorded outputs
// when the environment declares an identity and a cache is configured; without
// both, the order only keeps the two stages from interleaving.
func sharedPreparation(a, b PlannedCheck) bool {
	if a.Preparation == nil || b.Preparation == nil {
		return false
	}
	return digest(a.Preparation) == digest(b.Preparation)
}

// Execute runs a plan's checks, at most jobs of them at once; jobs zero keeps
// the default cap.
func Execute(ctx context.Context, plan Plan, shared string, executors map[ExecutorKind]Executor, jobs int) Report {
	results := make([]Result, len(plan.Checks))
	workers := checkParallelism(len(plan.Checks), jobs)
	slots := make(chan struct{}, workers)
	done := make([]chan struct{}, len(plan.Checks))
	for i := range done {
		done[i] = make(chan struct{})
	}

	var wg sync.WaitGroup
	for i, check := range plan.Checks {
		wg.Add(1)
		go func(i int, check PlannedCheck) {
			defer wg.Done()
			defer close(done[i])

			// Wait outside the worker slot so shared-prep chains cannot deadlock
			// the bounded pool (later check holds a slot while waiting on earlier).
			for j := 0; j < i; j++ {
				if sharedPreparation(plan.Checks[j], check) {
					<-done[j]
				}
			}

			slots <- struct{}{}
			defer func() { <-slots }()

			start := time.Now()
			outcome := executeCheck(ctx, check, Request{
				Source:       plan.Source,
				Shared:       shared,
				RerunChecks:  plan.RerunChecks,
				PlannedCheck: check,
			}, executors)
			duration := time.Since(start).Milliseconds()
			verifiedAt, executionMS := outcome.VerifiedAt, outcome.ExecutionMS
			if verifiedAt.IsZero() {
				verifiedAt, executionMS = start.UTC(), duration
			}

			results[i] = Result{
				ID:          check.ID,
				Status:      outcome.Status,
				DurationMS:  duration,
				VerifiedAt:  verifiedAt,
				Stdout:      outcome.Stdout,
				Stderr:      outcome.Stderr,
				Error:       outcome.Error,
				Cache:       outcome.Cache,
				ExecutionMS: executionMS,
				Stages:      outcome.Stages,
				Details:     outcome.Details,
			}
		}(i, check)
	}
	wg.Wait()

	status := StatusPassed
	if len(plan.Checks) == 0 {
		status = StatusIncomplete
	}
	for _, outcome := range results {
		if outcome.Status != StatusPassed {
			status = StatusFailed
			break
		}
	}

	return Report{Version: 1, Run: plan.Run, Status: status, Plan: plan, Results: results}
}

func executeCheck(ctx context.Context, check PlannedCheck, req Request, executors map[ExecutorKind]Executor) Result {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusCancelled, Error: err.Error()}
	}
	executor := executors[check.Environment.Executor]
	if executor == nil {
		return Result{Status: StatusIncomplete, Error: fmt.Sprintf("executor %q is unavailable", check.Environment.Executor)}
	}

	result := executor.Execute(ctx, req)
	switch result.Status {
	case StatusPassed, StatusFailed, StatusError, StatusCancelled, StatusIncomplete:
		return result
	default:
		return result.withOutcome(StatusError, "executor returned an invalid status")
	}
}

// The value receiver is the caller's copy, so each helper replaces its own
// fields and returns that copy with every other diagnostic and metadata field
// carried over untouched.

// withOutcome preserves diagnostics and metadata while replacing the outcome.
func (r Result) withOutcome(status Status, message string) Result {
	r.Status = status
	r.Error = message
	return r
}

func (r Result) withStages(stages []StageResult) Result {
	r.Stages = stages
	return r
}

func (r Result) withCache(cache CacheInfo) Result {
	r.Cache = cache
	return r
}
