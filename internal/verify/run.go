package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

//levenshtein:record
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

//levenshtein:record
type Report struct {
	Version int      `json:"version"`
	Run     string   `json:"run"`
	Status  Status   `json:"status"`
	Plan    Plan     `json:"plan"`
	Results []Result `json:"results"`
}

type Request struct {
	Source, Shared string
	Fresh          bool
	PlannedCheck
}

type Executor interface {
	Execute(context.Context, Request) Result
}

func Execute(ctx context.Context, plan Plan, shared string, executors map[ExecutorKind]Executor) Report {
	status := StatusPassed
	results := []Result{}
	for _, check := range plan.Checks {
		start := time.Now()
		outcome := executeCheck(ctx, check, Request{Source: plan.Source, Shared: shared, Fresh: plan.Fresh, PlannedCheck: check}, executors)
		duration := time.Since(start).Milliseconds()
		verifiedAt, executionMS := outcome.VerifiedAt, outcome.ExecutionMS
		if verifiedAt.IsZero() {
			verifiedAt, executionMS = start.UTC(), duration
		}

		results = append(results, Result{
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
		})
		if outcome.Status != StatusPassed {
			status = StatusFailed
		}
	}

	if len(plan.Checks) == 0 {
		status = StatusIncomplete
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

// withOutcome preserves diagnostics and metadata while replacing the outcome.
func (r Result) withOutcome(status Status, message string) Result {
	return Result{
		ID:          r.ID,
		Status:      status,
		DurationMS:  r.DurationMS,
		VerifiedAt:  r.VerifiedAt,
		Stdout:      r.Stdout,
		Stderr:      r.Stderr,
		Error:       message,
		Cache:       r.Cache,
		ExecutionMS: r.ExecutionMS,
		Stages:      r.Stages,
		Details:     r.Details,
	}
}

func (r Result) withStages(stages []StageResult) Result {
	return Result{
		ID:          r.ID,
		Status:      r.Status,
		DurationMS:  r.DurationMS,
		VerifiedAt:  r.VerifiedAt,
		Stdout:      r.Stdout,
		Stderr:      r.Stderr,
		Error:       r.Error,
		Cache:       r.Cache,
		ExecutionMS: r.ExecutionMS,
		Stages:      stages,
		Details:     r.Details,
	}
}

func (r Result) withCache(cache CacheInfo) Result {
	return Result{
		ID:          r.ID,
		Status:      r.Status,
		DurationMS:  r.DurationMS,
		VerifiedAt:  r.VerifiedAt,
		Stdout:      r.Stdout,
		Stderr:      r.Stderr,
		Error:       r.Error,
		Cache:       cache,
		ExecutionMS: r.ExecutionMS,
		Stages:      r.Stages,
		Details:     r.Details,
	}
}
