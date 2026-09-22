// Package verify plans and executes repository verification independently of CI.
package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Dir       string   `json:"dir"`
	Workspace string   `json:"workspace"`
	Inputs    []string `json:"inputs"`
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

type Check struct {
	Kind         CheckKind         `json:"kind"`
	Target       string            `json:"target"`
	Environment  string            `json:"environment"`
	Command      []string          `json:"command,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	Preparation  string            `json:"preparation,omitempty"`
	Artifacts    []string          `json:"artifacts,omitempty"`
	Cache        bool              `json:"cache,omitempty"`
	Build        string            `json:"build,omitempty"`
	RerunCommand []string          `json:"rerun_command,omitempty"`
	Base         string            `json:"base,omitempty"`
	Model        string            `json:"model,omitempty"`
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
	if err := d.Decode(new(any)); err != io.EOF {
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
	lint, vet, vuln := string(CheckGoLint), string(CheckGoVet), string(CheckGoVuln)
	checks := map[string]Check{}
	runs := map[string]Run{
		"branch":    {Checks: []string{lint, vet}},
		"pre-merge": {Checks: []string{lint, vet}},
		"main":      {Checks: []string{lint, vet, vuln}, RerunChecks: true},
	}
	for _, kind := range []CheckKind{CheckGoLint, CheckGoVet, CheckGoHTTP, CheckGoSQL, CheckGoVuln, CheckWorkflowLint} {
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
		return Config{}, err
	}
	return cfg, nil
}
