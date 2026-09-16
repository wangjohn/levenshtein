package verify

import (
	"fmt"
	"os"
	"path/filepath"
)

type PlannedCheck struct {
	ID          string       `json:"id"`
	Check       Check        `json:"check"`
	Target      Target       `json:"target"`
	Environment Environment  `json:"environment"`
	Preparation *Preparation `json:"preparation,omitempty"`
	Build       *Preparation `json:"build,omitempty"`
}

type Plan struct {
	Version int            `json:"version"`
	Run     string         `json:"run"`
	Fresh   bool           `json:"fresh"`
	Source  string         `json:"source"`
	Checks  []PlannedCheck `json:"checks"`
}

func (cfg Config) Plan(source, name string) (Plan, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return Plan{}, err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return Plan{}, err
	}

	run, ok := cfg.Runs[name]
	if !ok || len(run.Checks) == 0 {
		return Plan{}, fmt.Errorf("run %q is missing or empty", name)
	}

	p := Plan{Version: 1, Run: name, Fresh: run.Fresh, Source: source}
	seen := map[string]bool{}
	for _, id := range run.Checks {
		if id == "" || seen[id] {
			return p, fmt.Errorf("empty or duplicate check %q", id)
		}
		seen[id] = true
		check, ok := cfg.Checks[id]
		if !ok {
			return p, fmt.Errorf("unknown check %q", id)
		}
		target, ok := cfg.Targets[check.Target]
		if !ok {
			return p, fmt.Errorf("check %q: unknown target %q", id, check.Target)
		}
		env, ok := cfg.Environments[check.Environment]
		if !ok {
			return p, fmt.Errorf("check %q: unknown environment %q", id, check.Environment)
		}

		if err := validateCheck(check, env); err != nil {
			return p, fmt.Errorf("check %q: %w", id, err)
		}
		if run.Fresh && env.Executor == ExecutorNative && len(check.FreshCommand) == 0 {
			return p, fmt.Errorf("check %q: fresh native runs require an explicit fresh_command", id)
		}

		if target.Workspace == "" {
			target.Workspace = "."
		}
		for _, path := range []string{target.Dir, target.Workspace} {
			full, err := contained(source, path)
			if err != nil {
				return p, err
			}
			info, err := os.Stat(full)
			if err != nil {
				return p, err
			}
			if !info.IsDir() {
				return p, fmt.Errorf("%q is not a directory", path)
			}
		}

		if len(target.Inputs) == 0 {
			return p, fmt.Errorf("target %q must declare input paths", check.Target)
		}
		for _, path := range target.Inputs {
			if !relative(path) {
				return p, fmt.Errorf("invalid input path %q", path)
			}
		}

		planned := PlannedCheck{ID: id, Check: check, Target: target, Environment: env}
		planned.Preparation, err = resolveStage(cfg.Preparations, "preparation", check.Preparation)
		if err != nil {
			return p, err
		}
		planned.Build, err = resolveStage(cfg.Builds, "build", check.Build)
		if err != nil {
			return p, err
		}
		p.Checks = append(p.Checks, planned)
	}
	return p, nil
}
