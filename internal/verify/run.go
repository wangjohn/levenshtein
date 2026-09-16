package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Result struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	DurationMS int64           `json:"duration_ms"`
	VerifiedAt time.Time       `json:"verified_at"`
	Stdout     string          `json:"stdout,omitempty"`
	Stderr     string          `json:"stderr,omitempty"`
	Error      string          `json:"error,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
}
type Report struct {
	Version int      `json:"version"`
	Run     string   `json:"run"`
	Status  string   `json:"status"`
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

func Execute(ctx context.Context, plan Plan, shared string, executors map[string]Executor) Report {
	r := Report{Version: 1, Run: plan.Run, Status: "passed", Plan: plan, Results: []Result{}}
	for _, check := range plan.Checks {
		result := Result{ID: check.ID, Status: "incomplete"}
		start := time.Now()
		if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Error = ctx.Err().Error()
		} else if executor := executors[check.Environment.Executor]; executor != nil {
			result = executor.Execute(ctx, Request{Source: plan.Source, Shared: shared, Fresh: plan.Fresh, PlannedCheck: check})
			result.ID = check.ID
		} else {
			result.Error = fmt.Sprintf("executor %q is unavailable", check.Environment.Executor)
		}
		switch result.Status {
		case "passed", "failed", "error", "cancelled", "incomplete":
		default:
			result.Status = "error"
			result.Error = "executor returned an invalid status"
		}
		result.DurationMS = time.Since(start).Milliseconds()
		result.VerifiedAt = start.UTC()
		r.Results = append(r.Results, result)
		if result.Status != "passed" {
			r.Status = "failed"
		}
	}
	if len(plan.Checks) == 0 {
		r.Status = "incomplete"
	}
	return r
}
