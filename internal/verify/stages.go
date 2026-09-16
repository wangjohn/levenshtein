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

type stageEntry struct{ Key, Outputs string }

func (n *Native) stage(ctx context.Context, req Request, kind StageKind, stage *Preparation) (StageResult, *Result) {
	start := time.Now()
	req.Fresh = false // Freshness concerns verification, not reusable preparation.
	inputs, err := snapshot(req.Source, stage.Inputs, stage.Outputs, false)
	if err != nil {
		r := Result{Status: StatusError, Error: "stage inputs: " + err.Error()}
		return StageResult{Kind: kind}, &r
	}
	impl, err := implementation(req)
	if err != nil {
		r := Result{Status: StatusError, Error: err.Error()}
		return StageResult{Kind: kind}, &r
	}

	env := nativeEnv(req, stage.Env)
	// Build keys also include dependency preparation, whose inputs may be outside
	// the build source scope. Environment/version selection is always explicit.
	dependency := ""
	if kind == StageBuild && req.Preparation != nil {
		d, err := snapshot(req.Source, req.Preparation.Inputs, req.Preparation.Outputs, false)
		if err != nil {
			r := Result{Status: StatusError, Error: err.Error()}
			return StageResult{Kind: kind}, &r
		}
		dependency = digest([]any{req.Preparation, d})
	}

	key := digest([]any{kind, req.Source, req.Target.Workspace, stage, inputs, env, req.Environment, runtime.GOOS, runtime.GOARCH, impl, dependency})
	info := StageResult{Kind: kind, Key: key}

	path := ""
	var prior stageEntry
	if n.Cache != nil && req.Environment.Identity != "" {
		path = filepath.Join(n.Cache.Dir, "stages", key+".json")
		_ = readRecord(path, &prior)
	} else if req.Environment.Identity != "" && n.stages != nil {
		prior = n.stages[key]
	}
	if prior.Key == key && outputsExist(req.Source, stage.Outputs) {
		output, err := snapshot(req.Source, stage.Outputs, nil, true)
		if err == nil && output == prior.Outputs {
			return StageResult{Kind: kind, Key: key, Reused: true, DurationMS: time.Since(start).Milliseconds()}, nil
		}
	}

	for _, out := range stage.Outputs {
		if _, err := outputPath(req.Source, out); err != nil {
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
	output, err := snapshot(req.Source, stage.Outputs, nil, true)
	if err != nil {
		failure := result.withOutcome(StatusError, err.Error())
		return info, &failure
	}

	after, err := snapshot(req.Source, stage.Inputs, stage.Outputs, false)
	if err == nil && after == inputs && req.Environment.Identity != "" {
		record := stageEntry{Key: key, Outputs: output}
		if path != "" {
			_ = writeRecord(path, record)
		} else {
			if n.stages == nil {
				n.stages = map[string]stageEntry{}
			}
			n.stages[key] = record
		}
	}
	return StageResult{Kind: kind, Key: key, DurationMS: time.Since(start).Milliseconds()}, nil
}
