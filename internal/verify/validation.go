package verify

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A check carries at most one options object, and its kind decides which one.
// Validation therefore asks whether the right object is present rather than
// listing every field the other kinds would have used.
func validateCheck(check Check, env Environment) error {
	if env.Executor == ExecutorDagger {
		return validateDaggerCheck(check, env)
	}
	if env.Executor != ExecutorNative {
		return fmt.Errorf("unsupported executor %q", env.Executor)
	}

	kind, ok := nativeKinds[check.Kind]
	if !ok {
		return fmt.Errorf("native environments run %s checks, not %q", nativeKindNames(), check.Kind)
	}
	if err := validateEnvironment(env); err != nil {
		return err
	}
	return kind.validate(check, env)
}

func validateDaggerCheck(check Check, env Environment) error {
	if daggerFunctions[check.Kind] == "" {
		return fmt.Errorf("unknown Dagger check %q", check.Kind)
	}
	if check.Command != nil || check.Semantic != nil {
		return fmt.Errorf("command and semantic options cannot be used for Dagger Go checks")
	}
	if env.Identity != "" || len(env.Env) > 0 || len(env.PassEnv) > 0 || len(env.Tools) > 0 {
		return fmt.Errorf("native environment options cannot be used for Dagger Go checks")
	}
	return nil
}

func validateCommandCheck(check Check, env Environment) error {
	if check.Semantic != nil {
		return fmt.Errorf("semantic options apply only to semantic-lint checks")
	}
	command := check.Command
	if command == nil || len(command.Args) == 0 || command.Args[0] == "" {
		return fmt.Errorf("command check needs a nonempty args array")
	}
	if command.Cache && (env.Identity == "" || len(command.RerunArgs) == 0) {
		return fmt.Errorf("cacheable command check needs environment identity and explicit rerun_args")
	}
	if len(command.RerunArgs) > 0 && command.RerunArgs[0] == "" {
		return fmt.Errorf("rerun_args cannot be empty")
	}
	if err := validateDuration(command.Timeout); err != nil {
		return err
	}
	if err := validateEnv(command.Env); err != nil {
		return err
	}

	for _, path := range command.Artifacts {
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
	if check.Command != nil {
		return fmt.Errorf("semantic-lint does not accept command options")
	}
	if check.Semantic == nil {
		return nil
	}

	if err := validateDuration(check.Semantic.Timeout); err != nil {
		return err
	}
	if check.Semantic.Model != "" && !semanticModel.MatchString(check.Semantic.Model) {
		return fmt.Errorf("semantic-lint model %q must be a pinned release such as jev-1.13.0", check.Semantic.Model)
	}
	if check.Semantic.Base != "" && (strings.HasPrefix(check.Semantic.Base, "-") || strings.ContainsAny(check.Semantic.Base, " \t\n\x00")) {
		return fmt.Errorf("invalid semantic-lint base %q", check.Semantic.Base)
	}
	return nil
}

func validateEnvironment(env Environment) error {
	if err := validateEnv(env.Env); err != nil {
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
