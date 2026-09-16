package verify

import (
	"fmt"
	"strings"
	"time"
)

func validateCheck(check Check, env Environment) error {
	if env.Executor == "dagger" {
		if daggerFunctions[check.Kind] == "" {
			return fmt.Errorf("unknown Dagger check %q", check.Kind)
		}
		if check.Build != "" || len(check.FreshCommand) > 0 || len(check.Command) > 0 || len(check.Env) > 0 || check.Timeout != "" || check.Preparation != "" || len(check.Artifacts) > 0 || env.Identity != "" || len(env.Env) > 0 || len(env.PassEnv) > 0 || len(env.Tools) > 0 {
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
	if check.Cache && (env.Identity == "" || len(check.FreshCommand) == 0) {
		return fmt.Errorf("cacheable native check needs environment identity and explicit fresh_command")
	}
	if len(check.FreshCommand) > 0 && check.FreshCommand[0] == "" {
		return fmt.Errorf("fresh_command cannot be empty")
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

// A preparation or build is optional, but a named stage must be complete.
func resolveStage(stages map[string]Preparation, kind, name string) (*Preparation, error) {
	if name == "" {
		return nil, nil
	}
	stage, ok := stages[name]
	if !ok {
		return nil, fmt.Errorf("unknown %s %q", kind, name)
	}
	if len(stage.Command) == 0 || len(stage.Inputs) == 0 || len(stage.Outputs) == 0 {
		return nil, fmt.Errorf("%s must declare command, inputs, and outputs", kind)
	}
	if err := validateDuration(stage.Timeout); err != nil {
		return nil, err
	}
	if err := validateEnv(stage.Env); err != nil {
		return nil, err
	}
	for _, paths := range [][]string{stage.Inputs, stage.Outputs} {
		for _, path := range paths {
			if !relative(path) {
				return nil, fmt.Errorf("invalid %s path %q", kind, path)
			}
		}
	}
	return &stage, nil
}
