package verify

import (
	"slices"
	"testing"
)

func TestSecretsEnvDropsHostConfiguration(t *testing.T) {
	env := secretsEnv([]string{"PATH=/bin", "GITLEAKS_CONFIG=/elsewhere.toml", "GITLEAKS_CONFIG_TOML=[extend]", "HOME=/home/user"})
	if !slices.Equal(env, []string{"PATH=/bin", "HOME=/home/user"}) {
		t.Fatalf("unexpected gitleaks environment: %v", env)
	}
}

// secrets depends only on the declared files, the configuration, and the
// pinned gitleaks, so a pass is reused like shell-lint's.
func TestSecretsPassesAreReused(t *testing.T) {
	assertPassReused(t, CheckSecrets)
}
