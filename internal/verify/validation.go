package verify

import "fmt"

func validateCheck(check Check, env Environment) error {
	if env.Executor != ExecutorDagger {
		return fmt.Errorf("unsupported executor %q", env.Executor)
	}
	if daggerFunctions[check.Kind] == "" {
		return fmt.Errorf("unknown check kind %q", check.Kind)
	}
	return nil
}
