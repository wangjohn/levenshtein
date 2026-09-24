// Package verify plans and executes repository verification independently of CI.
package verify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

type Config struct {
	Version      int                    `json:"version"`
	Targets      map[string]Target      `json:"targets"`
	Environments map[string]Environment `json:"environments"`
	Checks       map[string]Check       `json:"checks"`
	Runs         map[string]Run         `json:"runs"`
	Preparations map[string]Preparation `json:"preparations,omitempty"`
	Builds       map[string]Preparation `json:"builds,omitempty"`
}

type Target struct {
	Dir       string        `json:"dir"`
	Workspace string        `json:"workspace"`
	Inputs    []string      `json:"inputs"`
	Exclude   []string      `json:"exclude,omitempty"`
	Discovery DiscoveryKind `json:"discovery,omitempty"`
}

type Environment struct {
	Executor ExecutorKind      `json:"executor"`
	Identity string            `json:"identity,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	PassEnv  []string          `json:"pass_env,omitempty"`
	Tools    []Tool            `json:"tools,omitempty"`
}

type Tool struct {
	Command []string `json:"command"`
	Version string   `json:"version"`
}

type Preparation struct {
	Command []string          `json:"command"`
	Inputs  []string          `json:"inputs"`
	Outputs []string          `json:"outputs"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
}

// A Check carries only the options its kind accepts. Most Dagger kinds take no
// object, a command check requires Command, semantic-lint may carry Semantic,
// go-mutation may carry Mutation, and go-lint may carry Lint on either
// executor. The other combinations cannot be written down.
//
// A check names one target, either with Target or as a Targets list that
// expands to one planned check per entry. Exactly one of the two is set; see
// Plan.
type Check struct {
	Kind        CheckKind      `json:"kind"`
	Target      string         `json:"target,omitempty"`
	Targets     []string       `json:"targets,omitempty"`
	Environment string         `json:"environment"`
	Command     *CommandCheck  `json:"command,omitempty"`
	Semantic    *SemanticCheck `json:"semantic,omitempty"`
	Mutation    *MutationCheck `json:"mutation,omitempty"`
	Lint        *LintCheck     `json:"lint,omitempty"`
}

// at binds a multi-target check to one of its targets, so every planned check
// names the single target it runs against.
func (check Check) at(target string) Check {
	check.Target = target
	check.Targets = nil
	return check
}

// CommandCheck runs a repository command on the native executor.
type CommandCheck struct {
	Args        []string          `json:"args"`
	RerunArgs   []string          `json:"rerun_args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
	Preparation string            `json:"preparation,omitempty"`
	Build       string            `json:"build,omitempty"`
	Artifacts   []string          `json:"artifacts,omitempty"`
	Cache       bool              `json:"cache,omitempty"`
}

// SemanticCheck tunes the advisory semantic-lint kind. Every field is optional.
type SemanticCheck struct {
	Base    string `json:"base,omitempty"`
	Model   string `json:"model,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// MutationScope selects which files a go-mutation check mutates.
type MutationScope string

const (
	// MutationScopeChanged mutates the files the branch changed against its base.
	MutationScopeChanged MutationScope = "changed"
	// MutationScopeModule mutates every eligible file in the target.
	MutationScopeModule MutationScope = "module"
)

// MutationCheck tunes the go-mutation kind. Every field is optional.
type MutationCheck struct {
	Base     string        `json:"base,omitempty"`
	Scope    MutationScope `json:"scope,omitempty"`
	Accepted string        `json:"accepted,omitempty"`
	Tags     string        `json:"tags,omitempty"`
	Timeout  string        `json:"timeout,omitempty"`
}

// LintCheck tunes the go-lint kind. Checks are Staticcheck -checks patterns
// applied after the shipped selection in runner/toolchain.json, where the last
// matching pattern wins, so "gocognit" turns an opt-in rule on and "-unparam"
// turns a default rule off without restating the rest.
type LintCheck struct {
	Checks []string `json:"checks"`
}

// artifacts, env and cacheable read options that only a command check has, so
// every other kind reports the zero value instead of needing a nil test.
func (check Check) artifacts() []string {
	if check.Command == nil {
		return nil
	}
	return check.Command.Artifacts
}

func (check Check) env() map[string]string {
	if check.Command == nil {
		return nil
	}
	return check.Command.Env
}

func (check Check) cacheable() bool {
	return check.Command != nil && check.Command.Cache
}

// semanticOptions supplies defaults for a semantic-lint check that declares no
// options of its own.
func (check Check) semanticOptions() SemanticCheck {
	if check.Semantic == nil {
		return SemanticCheck{}
	}
	return *check.Semantic
}

// lintChecks is what a go-lint check adds to the shipped selection, or
// nothing when it declares no lint options.
func (check Check) lintChecks() []string {
	if check.Lint == nil {
		return nil
	}
	return check.Lint.Checks
}

// defaultAccepted is where a go-mutation check looks for accepted survivors
// when its configuration names no file.
const defaultAccepted = ".levenshtein/mutation-accepted.json"

// mutationOptions fills in the defaults a go-mutation check leaves out.
func (check Check) mutationOptions() MutationCheck {
	var options MutationCheck
	if check.Mutation != nil {
		options = *check.Mutation
	}
	if options.Scope == "" {
		options.Scope = MutationScopeChanged
	}
	if options.Accepted == "" {
		options.Accepted = defaultAccepted
	}
	return options
}

type Run struct {
	Checks      []string `json:"checks"`
	RerunChecks bool     `json:"rerun_checks,omitempty"`
}

func decode(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}

// defaultTarget is the single target of the no-configuration defaults. It spans
// the whole source tree so an unconfigured repository needs no path knowledge.
const defaultTarget = "root"

// defaultEnvironment runs the no-configuration defaults through Dagger, which
// carries its own pinned Go toolchain.
const defaultEnvironment = "go"

// defaultConfig is the configuration a repository without levenshtein.json
// receives: every shared Dagger check over the whole source tree, the branch
// and pre-merge gates, a fresh main run, and one named run per check.
func defaultConfig() Config {
	lint, vet, mod, vuln := string(CheckGoLint), string(CheckGoVet), string(CheckGoMod), string(CheckGoVuln)
	checks := map[string]Check{}
	runs := map[string]Run{
		"branch":    {Checks: []string{lint, vet, mod}},
		"pre-merge": {Checks: []string{lint, vet, mod}},
		"main":      {Checks: []string{lint, vet, mod, vuln}, RerunChecks: true},
	}
	for _, kind := range []CheckKind{CheckGoLint, CheckGoVet, CheckGoMod, CheckGoTest, CheckGoHTTP, CheckGoSQL, CheckGoVuln, CheckWorkflowLint, CheckWorkflowSecurity, CheckShellLint} {
		checks[string(kind)] = Check{Kind: kind, Target: defaultTarget, Environment: defaultEnvironment}
		runs[string(kind)] = Run{Checks: []string{string(kind)}}
	}

	return Config{
		Version:      1,
		Targets:      map[string]Target{defaultTarget: {Dir: ".", Workspace: ".", Inputs: []string{"."}}},
		Environments: map[string]Environment{defaultEnvironment: {Executor: ExecutorDagger}},
		Checks:       checks,
		Runs:         runs,
	}
}

func Load(source string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(source, "levenshtein.json"))
	if os.IsNotExist(err) {
		return defaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

// Parse reads the versioned configuration interface. Version 1 is the only
// accepted shape; a file without it is rejected rather than guessed at.
func Parse(data []byte) (Config, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Config{}, err
	}
	if header.Version != 1 {
		return Config{}, fmt.Errorf(`configuration needs "version": 1; see docs/configuration.md`)
	}

	var cfg Config
	if err := decode(data, &cfg); err != nil {
		if hint := migrationHint(data); hint != "" {
			return Config{}, fmt.Errorf("%s: %w", hint, err)
		}
		return Config{}, err
	}
	return cfg, nil
}

// Check options that used to sit directly on a check now live in its command
// or semantic object. A file written for the earlier layout fails to decode;
// migrationHint turns that raw decoder error into a pointer at the move.
var (
	retiredCommandFields  = []string{"rerun_command", "artifacts", "cache", "preparation", "build", "env"}
	retiredSemanticFields = []string{"base", "model"}
)

func migrationHint(data []byte) string {
	var loose struct {
		Checks map[string]map[string]json.RawMessage `json:"checks"`
	}
	if json.Unmarshal(data, &loose) != nil {
		return ""
	}

	for _, id := range slices.Sorted(maps.Keys(loose.Checks)) {
		check := loose.Checks[id]
		var kind CheckKind
		_ = json.Unmarshal(check["kind"], &kind)
		if raw, ok := check["command"]; ok && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			return fmt.Sprintf(`check %q: "command" is now an object; write "command": {"args": [...]} (see docs/configuration.md)`, id)
		}
		for _, field := range append(append([]string{"timeout"}, retiredCommandFields...), retiredSemanticFields...) {
			if _, ok := check[field]; ok {
				return fmt.Sprintf("check %q: %s (see docs/configuration.md)", id, retiredFieldHome(kind, field))
			}
		}
	}
	return ""
}

// retiredFieldHome says where a retired top-level check field went for the
// check's kind, or that it no longer applies.
func retiredFieldHome(kind CheckKind, field string) string {
	if daggerFunctions[kind] != "" {
		if field == "cache" {
			return `"cache" no longer applies: Dagger results are always cached; remove it`
		}
		return fmt.Sprintf("%q does not apply to Dagger checks; remove it", field)
	}
	if kind == CheckSemanticLint {
		if field == "env" {
			return `"env" is no longer accepted on a semantic-lint check; declare variables in the environment's "env"`
		}
		return fmt.Sprintf(`%q now lives inside the "semantic" object`, field)
	}
	if field == "rerun_command" {
		return `"rerun_command" is now "rerun_args" inside the "command" object`
	}
	return fmt.Sprintf(`%q now lives inside the "command" object`, field)
}
