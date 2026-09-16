// Package verify plans and executes repository verification independently of CI.
package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Version      int                    `json:"version"`
	Targets      map[string]Target      `json:"targets"`
	Environments map[string]Environment `json:"environments"`
	Checks       map[string]Check       `json:"checks"`
	Runs         map[string]Run         `json:"runs"`
	Preparations map[string]Preparation `json:"preparations,omitempty"`
}
type Target struct {
	Dir       string   `json:"dir"`
	Workspace string   `json:"workspace"`
	Inputs    []string `json:"inputs"`
}
type Environment struct {
	Executor string            `json:"executor"`
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
type Check struct {
	Kind        string            `json:"kind"`
	Target      string            `json:"target"`
	Environment string            `json:"environment"`
	Command     []string          `json:"command,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
	Preparation string            `json:"preparation,omitempty"`
	Artifacts   []string          `json:"artifacts,omitempty"`
}
type Run struct {
	Checks []string `json:"checks"`
	Fresh  bool     `json:"fresh,omitempty"`
}

func decode(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}
func Load(source string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(source, "levenshtein.json"))
	if os.IsNotExist(err) {
		data = []byte(`{"modules":["."],"runs":{"branch":["go-lint"],"pre-merge":["go-lint"],"main":["go-lint"],"go-lint":["go-lint"]}}`)
	} else if err != nil {
		return Config{}, err
	}
	return Parse(data)
}
func Parse(data []byte) (Config, error) {
	var header map[string]json.RawMessage
	if err := json.Unmarshal(data, &header); err != nil {
		return Config{}, err
	}
	if _, ok := header["version"]; ok {
		var cfg Config
		if err := decode(data, &cfg); err != nil {
			return cfg, err
		}
		if cfg.Version != 1 {
			return cfg, fmt.Errorf("unsupported configuration version %d", cfg.Version)
		}
		return cfg, nil
	}
	var old struct {
		Modules []string            `json:"modules"`
		Runs    map[string][]string `json:"runs"`
	}
	if err := decode(data, &old); err != nil {
		return Config{}, err
	}
	if len(old.Modules) == 0 {
		return Config{}, fmt.Errorf("configure at least one Go module directory")
	}
	cfg := Config{Version: 1, Targets: map[string]Target{}, Environments: map[string]Environment{"go": {Executor: "dagger"}}, Checks: map[string]Check{}, Runs: map[string]Run{}}
	seen := map[string]bool{}
	for i, module := range old.Modules {
		if !relative(module) || seen[module] {
			return Config{}, fmt.Errorf("invalid or duplicate module %q", module)
		}
		seen[module] = true
		id := fmt.Sprintf("module-%d", i)
		cfg.Targets[id] = Target{Dir: module, Workspace: ".", Inputs: []string{"."}}
		cfg.Checks["go-lint/"+id] = Check{Kind: "go-lint", Target: id, Environment: "go"}
	}
	cfg.Checks["self-test"] = Check{Kind: "self-test", Target: "module-0", Environment: "go"}
	for name, checks := range old.Runs {
		run := Run{Fresh: name == "main"}
		seen := map[string]bool{}
		for _, check := range checks {
			if seen[check] {
				return Config{}, fmt.Errorf("duplicate check %q", check)
			}
			seen[check] = true
			switch check {
			case "self-test":
				run.Checks = append(run.Checks, check)
			case "go-lint":
				for i := range old.Modules {
					run.Checks = append(run.Checks, fmt.Sprintf("go-lint/module-%d", i))
				}
			default:
				return Config{}, fmt.Errorf("unknown legacy check %q", check)
			}
		}
		cfg.Runs[name] = run
	}
	return cfg, nil
}

type PlannedCheck struct {
	ID          string       `json:"id"`
	Check       Check        `json:"check"`
	Target      Target       `json:"target"`
	Environment Environment  `json:"environment"`
	Preparation *Preparation `json:"preparation,omitempty"`
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
		if check.Preparation != "" {
			preparation, ok := cfg.Preparations[check.Preparation]
			if !ok {
				return p, fmt.Errorf("unknown preparation %q", check.Preparation)
			}
			if len(preparation.Command) == 0 || len(preparation.Inputs) == 0 || len(preparation.Outputs) == 0 {
				return p, fmt.Errorf("preparation must declare command, inputs, and outputs")
			}
			if err := validateDuration(preparation.Timeout); err != nil {
				return p, err
			}
			if err := validateEnv(preparation.Env); err != nil {
				return p, err
			}
			for _, path := range append(append([]string{}, preparation.Inputs...), preparation.Outputs...) {
				if !relative(path) {
					return p, fmt.Errorf("invalid preparation path %q", path)
				}
			}
			planned.Preparation = &preparation
		}
		p.Checks = append(p.Checks, planned)
	}
	return p, nil
}
func validateCheck(check Check, env Environment) error {
	if env.Executor == "dagger" {
		if check.Kind != "go-lint" && check.Kind != "self-test" {
			return fmt.Errorf("unknown Dagger check %q", check.Kind)
		}
		if len(check.Command) > 0 || len(check.Env) > 0 || check.Timeout != "" || check.Preparation != "" || len(check.Artifacts) > 0 || env.Identity != "" || len(env.Env) > 0 || len(env.PassEnv) > 0 || len(env.Tools) > 0 {
			return fmt.Errorf("native command options cannot be used for Dagger Go checks")
		}
		return nil
	}
	if env.Executor != "native" {
		return fmt.Errorf("unsupported executor %q", env.Executor)
	}
	if check.Kind != "command" || len(check.Command) == 0 || check.Command[0] == "" {
		return fmt.Errorf("native check needs kind command and a nonempty command array")
	}
	if err := validateDuration(check.Timeout); err != nil {
		return err
	}
	if err := validateEnv(env.Env); err != nil {
		return err
	}
	if err := validateEnv(check.Env); err != nil {
		return err
	}
	for _, name := range env.PassEnv {
		if !envName(name) {
			return fmt.Errorf("invalid environment name %q", name)
		}
	}
	for _, tool := range env.Tools {
		if len(tool.Command) == 0 || tool.Command[0] == "" || tool.Version == "" {
			return fmt.Errorf("tool needs command and exact version output")
		}
	}
	for _, path := range check.Artifacts {
		if !relative(path) || path == "." {
			return fmt.Errorf("invalid artifact path %q", path)
		}
	}
	return nil
}
func validateDuration(value string) error {
	if value == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid positive timeout %q", value)
	}
	return nil
}
func envName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "=\x00") && !strings.HasPrefix(name, "LEVENSHTEIN_")
}
func validateEnv(values map[string]string) error {
	for name, value := range values {
		if !envName(name) || strings.ContainsRune(value, 0) {
			return fmt.Errorf("invalid environment entry %q", name)
		}
	}
	return nil
}
