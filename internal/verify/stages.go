package verify

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

type namedStage struct {
	kind       string
	definition *Preparation
}

// All consumers use preparation-before-build order and skip absent stages.
func (check PlannedCheck) stages() []namedStage {
	var stages []namedStage
	if check.Preparation != nil {
		stages = append(stages, namedStage{"preparation", check.Preparation})
	}
	if check.Build != nil {
		stages = append(stages, namedStage{"build", check.Build})
	}
	return stages
}

type stageEntry struct{ Key, Outputs string }

func (n *Native) stage(ctx context.Context, req Request, kind string, stage *Preparation) (StageResult, *Result) {
	start := time.Now()
	info := StageResult{Kind: kind}
	req.Fresh = false // Freshness concerns verification, not reusable preparation.
	inputs, err := snapshot(req.Source, stage.Inputs, stage.Outputs, false)
	if err != nil {
		r := Result{Status: "error", Error: "stage inputs: " + err.Error()}
		return info, &r
	}
	impl, err := implementation(req)
	if err != nil {
		r := Result{Status: "error", Error: err.Error()}
		return info, &r
	}
	env := nativeEnv(req, stage.Env)
	// Build keys also include dependency preparation, whose inputs may be outside
	// the build source scope. Environment/version selection is always explicit.
	dependency := ""
	if kind == "build" && req.Preparation != nil {
		d, err := snapshot(req.Source, req.Preparation.Inputs, req.Preparation.Outputs, false)
		if err != nil {
			r := Result{Status: "error", Error: err.Error()}
			return info, &r
		}
		dependency = digest([]any{req.Preparation, d})
	}
	key := digest([]any{kind, req.Source, req.Target.Workspace, stage, inputs, env, req.Environment, runtime.GOOS, runtime.GOARCH, impl, dependency})
	info.Key = key
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
			info.Reused = true
			info.DurationMS = time.Since(start).Milliseconds()
			return info, nil
		}
	}
	for _, out := range stage.Outputs {
		if _, err := outputPath(req.Source, out); err != nil {
			r := Result{Status: "error", Error: err.Error()}
			return info, &r
		}
	}
	if r := validateTools(ctx, filepath.Join(req.Source, req.Target.Workspace), req.Environment.Tools, env); r != nil {
		return info, r
	}
	result := command(ctx, filepath.Join(req.Source, req.Target.Workspace), stage.Command, env, stage.Timeout)
	if result.Status != "passed" {
		result.Error = kind + ": " + result.Error
		return info, &result
	}
	if !outputsExist(req.Source, stage.Outputs) {
		result.Status = "error"
		result.Error = fmt.Sprintf("%s did not produce declared outputs", kind)
		return info, &result
	}
	output, err := snapshot(req.Source, stage.Outputs, nil, true)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return info, &result
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
	info.DurationMS = time.Since(start).Milliseconds()
	return info, nil
}
