package verify

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

func validateCheck(check Check, env Environment) error {
	if env.Executor == ExecutorDagger {
		if daggerFunctions[check.Kind] == "" {
			return fmt.Errorf("unknown Dagger check %q", check.Kind)
		}
		if check.Build != "" || len(check.RerunCommand) > 0 || len(check.Command) > 0 || len(check.Env) > 0 || check.Timeout != "" || check.Preparation != "" || len(check.Artifacts) > 0 || check.Base != "" || check.Model != "" || env.Identity != "" || len(env.Env) > 0 || len(env.PassEnv) > 0 || len(env.Tools) > 0 {
			return fmt.Errorf("native command options cannot be used for Dagger Go checks")
		}
		return nil
	}

	if env.Executor != ExecutorNative {
		return fmt.Errorf("unsupported executor %q", env.Executor)
	}
	if check.Kind == CheckSemanticLint {
		return validateSemanticLint(check, env)
	}
	if check.Kind != CheckCommand || len(check.Command) == 0 || check.Command[0] == "" {
		return fmt.Errorf("native check needs kind command and a nonempty command array")
	}
	if check.Cache && (env.Identity == "" || len(check.RerunCommand) == 0) {
		return fmt.Errorf("cacheable native check needs environment identity and explicit rerun_command")
	}
	if len(check.RerunCommand) > 0 && check.RerunCommand[0] == "" {
		return fmt.Errorf("rerun_command cannot be empty")
	}
	if check.Base != "" || check.Model != "" {
		return fmt.Errorf("base and model apply only to semantic-lint checks")
	}

	if err := validateNativeEnvironment(check, env); err != nil {
		return err
	}
	for _, path := range check.Artifacts {
		if !relative(path) || path == "." {
			return fmt.Errorf("invalid artifact path %q", path)
		}
	}
	return nil
}

// Thresholds are tuned per release, so aliases such as jev-latest are rejected.
var semanticModel = regexp.MustCompile(`^jev-[0-9]+\.[0-9]+\.[0-9]+$`)

// A semantic-lint check has no command of its own, always executes, and reads
// its API key from the environment rather than configuration.
func validateSemanticLint(check Check, env Environment) error {
	if len(check.Command) > 0 || len(check.RerunCommand) > 0 || len(check.Artifacts) > 0 || check.Preparation != "" || check.Build != "" {
		return fmt.Errorf("semantic-lint does not accept command, rerun_command, artifacts, preparation, or build")
	}
	if check.Cache {
		return fmt.Errorf("semantic-lint results are not cached")
	}
	if check.Model != "" && !semanticModel.MatchString(check.Model) {
		return fmt.Errorf("semantic-lint model %q must be a pinned release such as jev-1.13.0", check.Model)
	}
	if check.Base != "" && (strings.HasPrefix(check.Base, "-") || strings.ContainsAny(check.Base, " \t\n\x00")) {
		return fmt.Errorf("invalid semantic-lint base %q", check.Base)
	}
	if err := validateSemanticCredentials(check, env); err != nil {
		return err
	}
	return validateNativeEnvironment(check, env)
}

// validateSemanticCredentials keeps the API key and the API origin out of
// committed configuration. Configuration travels with the pull request, so a
// declared origin would otherwise decide where the CI secret is sent.
// Committed PATH or GIT_* values would choose which git runs and how it
// behaves, and so what the check sends; the host owns those too.
func validateSemanticCredentials(check Check, env Environment) error {
	for name := range env.Env {
		if name == "PATH" || strings.HasPrefix(name, "GIT_") {
			return fmt.Errorf("semantic-lint runs git from the host environment; remove %s from the environment's env", name)
		}
	}
	for name := range check.Env {
		if name == "PATH" || strings.HasPrefix(name, "GIT_") {
			return fmt.Errorf("semantic-lint runs git from the host environment; remove %s from the check's env", name)
		}
	}
	for _, name := range []string{semanticAPIKeyEnv, semanticBaseURLEnv} {
		if _, declared := env.Env[name]; declared {
			return fmt.Errorf("semantic-lint reads %s from the host environment; remove it from the environment's env, which is committed configuration", name)
		}
		if _, declared := check.Env[name]; declared {
			return fmt.Errorf("semantic-lint reads %s from the host environment; remove it from the check's env, which is committed configuration", name)
		}
	}
	for _, name := range env.PassEnv {
		if name == semanticAPIKeyEnv {
			return fmt.Errorf("semantic-lint reads %s itself; remove it from pass_env so the key is not passed to the check's subprocesses", name)
		}
		if name == "PATH" || strings.HasPrefix(name, "GIT_") {
			return fmt.Errorf("semantic-lint runs git from the host environment; remove %s from pass_env", name)
		}
	}
	return nil
}

func validateNativeEnvironment(check Check, env Environment) error {
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
