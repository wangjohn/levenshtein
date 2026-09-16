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
	FreshCommand []string          `json:"fresh_command,omitempty"`
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

	cfg := Config{Version: 1, Targets: map[string]Target{}, Environments: map[string]Environment{"go": {Executor: ExecutorDagger}}, Checks: map[string]Check{}, Runs: map[string]Run{}}
	seen := map[string]bool{}
	for i, module := range old.Modules {
		if !relative(module) || seen[module] {
			return Config{}, fmt.Errorf("invalid or duplicate module %q", module)
		}
		seen[module] = true
		id := fmt.Sprintf("module-%d", i)
		cfg.Targets[id] = Target{Dir: module, Workspace: ".", Inputs: []string{"."}}
		cfg.Checks["go-lint/"+id] = Check{Kind: CheckGoLint, Target: id, Environment: "go"}
	}

	cfg.Checks["self-test"] = Check{Kind: CheckSelfTest, Target: "module-0", Environment: "go"}

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
