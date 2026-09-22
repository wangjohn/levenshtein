package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	Version     int            `json:"version"`
	Run         string         `json:"run"`
	RerunChecks bool           `json:"rerun_checks"`
	Source      string         `json:"source"`
	Checks      []PlannedCheck `json:"checks"`
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

	p := Plan{Version: 1, Run: name, RerunChecks: run.RerunChecks, Source: source}
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
		if run.RerunChecks && env.Executor == ExecutorNative {
			if err := nativeKinds[check.Kind].rerunReady(check); err != nil {
				return p, fmt.Errorf("check %q: %w", id, err)
			}
		}

		if check.Kind == CheckWorkflowLint && target.Dir != "." {
			return p, fmt.Errorf("check %q: workflow-lint requires a repository-root target", id)
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
		for _, path := range target.Exclude {
			if !literalPath(path) || path == "." {
				return p, fmt.Errorf("target %q: exclude %q must be a literal repository-relative path without pattern characters", check.Target, path)
			}
		}

		// Discovery is explicit in the plan even when configuration omits it, so
		// a dry run says which enumeration produced a fingerprint.
		if target.Discovery == "" {
			target.Discovery = DiscoveryGit
		}
		if !slices.Contains(discoveryKinds, target.Discovery) {
			return p, fmt.Errorf("target %q: unknown discovery %q", check.Target, target.Discovery)
		}

		if env.Executor == ExecutorDagger {
			if _, err := daggerIncludes(target.Inputs); err != nil {
				return p, err
			}
		}

		// Only a command check can name stages; other kinds resolve to none.
		named := CommandCheck{}
		if check.Command != nil {
			named = *check.Command
		}
		preparation, err := resolveStage(cfg.Preparations, "preparation", named.Preparation)
		if err != nil {
			return p, err
		}
		build, err := resolveStage(cfg.Builds, "build", named.Build)
		if err != nil {
			return p, err
		}
		p.Checks = append(p.Checks, PlannedCheck{
			ID: id, Check: check, Target: target, Environment: env,
			Preparation: preparation, Build: build,
		})
	}
	return p, nil
}
