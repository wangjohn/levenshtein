package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	Baseline    string         `json:"baseline,omitempty"`
	Checks      []PlannedCheck `json:"checks"`
}

// A selection is one check a run asked for, after a multi-target check has been
// expanded: the ID the plan reports and the check bound to a single target.
type selection struct {
	ID    string
	Check Check
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

	if cfg.Baseline != "" {
		if err := validBaselinePath(cfg.Baseline); err != nil {
			return Plan{}, err
		}
	}

	p := Plan{Version: 1, Run: name, RerunChecks: run.RerunChecks, Source: source, Baseline: cfg.Baseline}
	seen := map[string]bool{}
	for _, reference := range run.Checks {
		selections, err := cfg.selections(reference)
		if err != nil {
			return p, err
		}

		for _, selected := range selections {
			if seen[selected.ID] {
				return p, fmt.Errorf("empty or duplicate check %q", selected.ID)
			}
			seen[selected.ID] = true

			planned, err := cfg.planCheck(source, selected, run.RerunChecks)
			if err != nil {
				return p, err
			}
			p.Checks = append(p.Checks, planned)
		}
	}
	return p, nil
}

// selections resolves one reference from a run. A reference is either a check
// ID, which selects every target that check declares, or "<id>/<target>", which
// selects a single target of a check declared with "targets". A check declared
// with the singular "target" is selected by its ID alone.
func (cfg Config) selections(reference string) ([]selection, error) {
	if check, ok := cfg.Checks[reference]; ok {
		if err := validateTargets(check); err != nil {
			return nil, fmt.Errorf("check %q: %w", reference, err)
		}
		if len(check.Targets) == 0 {
			return []selection{{ID: reference, Check: check}}, nil
		}

		selected := make([]selection, 0, len(check.Targets))
		for _, target := range check.Targets {
			selected = append(selected, selection{ID: reference + "/" + target, Check: check.at(target)})
		}
		return selected, nil
	}

	id, target := reference, ""
	if slash := strings.LastIndex(reference, "/"); slash > 0 {
		id, target = reference[:slash], reference[slash+1:]
	}
	check, ok := cfg.Checks[id]
	if !ok {
		return nil, fmt.Errorf("unknown check %q", reference)
	}
	if err := validateTargets(check); err != nil {
		return nil, fmt.Errorf("check %q: %w", id, err)
	}
	if len(check.Targets) == 0 {
		return nil, fmt.Errorf("check %q declares one target; reference it as %q", id, id)
	}
	if !slices.Contains(check.Targets, target) {
		return nil, fmt.Errorf("check %q declares no target %q", id, target)
	}
	return []selection{{ID: reference, Check: check.at(target)}}, nil
}

// A check declares its target exactly once, either as "target" or as a
// nonempty "targets" list without repeats.
func validateTargets(check Check) error {
	if (check.Target == "") == (len(check.Targets) == 0) {
		return fmt.Errorf(`needs exactly one of "target" and a nonempty "targets"`)
	}

	seen := map[string]bool{}
	for _, target := range check.Targets {
		if target == "" || seen[target] {
			return fmt.Errorf("empty or duplicate target %q", target)
		}
		seen[target] = true
	}
	return nil
}

// planCheck resolves one selected check against the configuration and the
// source tree. It starts no executor and runs none of the checks' own tools.
func (cfg Config) planCheck(source string, selected selection, rerunChecks bool) (PlannedCheck, error) {
	id, check := selected.ID, selected.Check
	target, ok := cfg.Targets[check.Target]
	if !ok {
		return PlannedCheck{}, fmt.Errorf("check %q: unknown target %q", id, check.Target)
	}
	env, ok := cfg.Environments[check.Environment]
	if !ok {
		return PlannedCheck{}, fmt.Errorf("check %q: unknown environment %q", id, check.Environment)
	}

	if err := validateCheck(check, env); err != nil {
		return PlannedCheck{}, fmt.Errorf("check %q: %w", id, err)
	}
	if rerunChecks && env.Executor == ExecutorNative {
		if err := nativeKinds[check.Kind].rerunReady(check); err != nil {
			return PlannedCheck{}, fmt.Errorf("check %q: %w", id, err)
		}
	}

	if (check.Kind == CheckWorkflowLint || check.Kind == CheckWorkflowSecurity || check.Kind == CheckShellLint || check.Kind == CheckSecrets || check.Kind == CheckDepsVuln) && target.Dir != "." {
		return PlannedCheck{}, fmt.Errorf("check %q: %s requires a repository-root target", id, check.Kind)
	}

	if target.Workspace == "" {
		target.Workspace = "."
	}
	for _, path := range []string{target.Dir, target.Workspace} {
		full, err := contained(source, path)
		if err != nil {
			return PlannedCheck{}, err
		}
		info, err := os.Stat(full)
		if err != nil {
			return PlannedCheck{}, err
		}
		if !info.IsDir() {
			return PlannedCheck{}, fmt.Errorf("%q is not a directory", path)
		}
	}

	if len(target.Inputs) == 0 {
		return PlannedCheck{}, fmt.Errorf("target %q must declare input paths", check.Target)
	}
	for _, path := range target.Inputs {
		if !relative(path) {
			return PlannedCheck{}, fmt.Errorf("invalid input path %q", path)
		}
	}
	for _, path := range target.Exclude {
		if !literalPath(path) || path == "." {
			return PlannedCheck{}, fmt.Errorf("target %q: exclude %q must be a literal repository-relative path without pattern characters", check.Target, path)
		}
	}

	// Discovery is explicit in the plan even when configuration omits it, so a
	// dry run says which enumeration produced a fingerprint.
	if target.Discovery == "" {
		target.Discovery = DiscoveryGit
	}
	if !slices.Contains(discoveryKinds, target.Discovery) {
		return PlannedCheck{}, fmt.Errorf("target %q: unknown discovery %q", check.Target, target.Discovery)
	}

	if env.Executor == ExecutorDagger {
		if _, err := daggerIncludes(target.Inputs); err != nil {
			return PlannedCheck{}, err
		}
	}

	// Only a command check can name stages; other kinds resolve to none.
	named := CommandCheck{}
	if check.Command != nil {
		named = *check.Command
	}
	preparation, err := resolveStage(cfg.Preparations, "preparation", named.Preparation)
	if err != nil {
		return PlannedCheck{}, err
	}
	build, err := resolveStage(cfg.Builds, "build", named.Build)
	if err != nil {
		return PlannedCheck{}, err
	}

	return PlannedCheck{
		ID: id, Check: check, Target: target, Environment: env,
		Preparation: preparation, Build: build,
	}, nil
}
