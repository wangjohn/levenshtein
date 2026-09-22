package verify

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

type namedStage struct {
	kind       StageKind
	definition *Preparation
}

// All consumers use preparation-before-build order and skip absent stages.
func (check PlannedCheck) stages() []namedStage {
	var stages []namedStage
	if check.Preparation != nil {
		stages = append(stages, namedStage{StagePreparation, check.Preparation})
	}
	if check.Build != nil {
		stages = append(stages, namedStage{StageBuild, check.Build})
	}
	return stages
}

type stageEntry struct {
	Key     string
	Outputs string
}

func (n *Native) stage(ctx context.Context, req Request, kind StageKind, stage *Preparation) (StageResult, *Result) {
	start := time.Now()
	req.RerunChecks = false // Freshness concerns verification, not reusable preparation.
	inputs, err := snapshot(ctx, snapshotRequest{Root: req.Source, Paths: stage.Inputs, Excludes: stage.Outputs, Discovery: req.Target.Discovery})
	if err != nil {
		r := Result{Status: StatusError, Error: "stage inputs: " + err.Error()}
		return StageResult{Kind: kind}, &r
	}
	impl, err := implementation(ctx, req)
	if err != nil {
		r := Result{Status: StatusError, Error: err.Error()}
		return StageResult{Kind: kind}, &r
	}

	env := nativeEnv(req, stage.Env)
	// Build keys also include dependency preparation, whose inputs may be outside
	// the build source scope. Environment/version selection is always explicit.
	dependency := ""
	if kind == StageBuild && req.Preparation != nil {
		d, err := snapshot(ctx, snapshotRequest{Root: req.Source, Paths: req.Preparation.Inputs, Excludes: req.Preparation.Outputs, Discovery: req.Target.Discovery})
		if err != nil {
			r := Result{Status: StatusError, Error: err.Error()}
			return StageResult{Kind: kind}, &r
		}
		dependency = digest([]any{req.Preparation, d})
	}

	key := digest([]any{kind, req.Source, req.Target.Workspace, stage, inputs, env, req.Environment, runtime.GOOS, runtime.GOARCH, impl, dependency})
	info := StageResult{Kind: kind, Key: key}

	// Only a pinned environment with a cache remembers a stage. Without both,
	// the stage runs for every check and keeps whatever incremental reuse its
	// own native tooling provides.
	path := ""
	var prior stageEntry
	if n.Cache != nil && req.Environment.Identity != "" {
		path = filepath.Join(n.Cache.Dir, "stages", key+".json")
		_ = readRecord(path, &prior)
	}
	if prior.Key == key && outputsExist(req.Source, stage.Outputs) {
		output, err := snapshot(ctx, snapshotRequest{Root: req.Source, Paths: stage.Outputs, Outputs: true})
		if err == nil && output == prior.Outputs {
			return StageResult{Kind: kind, Key: key, Reused: true, DurationMS: time.Since(start).Milliseconds()}, nil
		}
	}

	for _, out := range stage.Outputs {
		if err := checkOutputPath(req.Source, out); err != nil {
			r := Result{Status: StatusError, Error: err.Error()}
			return info, &r
		}
	}
	if r := validateTools(ctx, filepath.Join(req.Source, req.Target.Workspace), req.Environment.Tools, env); r != nil {
		return info, r
	}

	result := command(ctx, filepath.Join(req.Source, req.Target.Workspace), stage.Command, env, stage.Timeout)
	if result.Status != StatusPassed {
		failure := result.withOutcome(result.Status, string(kind)+": "+result.Error)
		return info, &failure
	}
	if !outputsExist(req.Source, stage.Outputs) {
		failure := result.withOutcome(StatusError, fmt.Sprintf("%s did not produce declared outputs", kind))
		return info, &failure
	}
	output, err := snapshot(ctx, snapshotRequest{Root: req.Source, Paths: stage.Outputs, Outputs: true})
	if err != nil {
		failure := result.withOutcome(StatusError, err.Error())
		return info, &failure
	}

	after, err := snapshot(ctx, snapshotRequest{Root: req.Source, Paths: stage.Inputs, Excludes: stage.Outputs, Discovery: req.Target.Discovery})
	if err == nil && after == inputs && path != "" {
		_ = writeRecord(path, stageEntry{Key: key, Outputs: output})
	}
	return StageResult{Kind: kind, Key: key, DurationMS: time.Since(start).Milliseconds()}, nil
}
