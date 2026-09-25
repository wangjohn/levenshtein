package verify

import (
	"slices"
	"testing"
)

// fakeSecret stands in for a leaked value in these tests. It is made up.
const fakeSecret = "9f8a7Qm2Lx0Zc4Vb6Nn1Ty8Ru3Ew5Qd" // gitleaks:allow

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
