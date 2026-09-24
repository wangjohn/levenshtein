//go:build integration

package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The native executor reaches the same verdicts as the Dagger self-test
// (runner/secrets.go) on the same fixtures. Building gitleaks needs the Go
// module proxy.
func TestNativeSecretsAgreesWithTheFixtures(t *testing.T) {
	shared := repositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cacheDir := t.TempDir()
	native := &Native{Cache: &Cache{Dir: cacheDir}}

	t.Run("secrets passes secrets-clean", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "secrets-clean", CheckSecrets))
		if result.Status != StatusPassed {
			t.Fatalf("secrets-clean fixture must pass secrets: %+v", result)
		}
	})

	t.Run("secrets fails secrets-leaky without ever recording the secret", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(shared, "runner", "testdata", "secrets-leaky", "settings.py"))
		if err != nil || !bytes.Contains(data, []byte(fakeSecret)) {
			t.Fatalf("the fixture must hold the made-up key this test looks for: %v", err)
		}

		cached := CachedExecutor{Cache: &Cache{Dir: cacheDir}, Executor: native}
		result := cached.Execute(ctx, sourceRequest(t, shared, filepath.Join(shared, "runner", "testdata", "secrets-leaky"), CheckSecrets))
		if result.Status != StatusFailed {
			t.Fatalf("secrets-leaky fixture must fail for its finding, not a tool error: %+v", result)
		}
		if got := findingLines(fixtureFindings(t, result)); !slices.Equal(got, []string{"generic-api-key settings.py:9:2"}) {
			t.Fatalf("secrets-leaky findings %v", got)
		}

		report, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(report, []byte(fakeSecret)) {
			t.Fatalf("the report carried the secret: %s", report)
		}
		err = filepath.WalkDir(cacheDir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err == nil && bytes.Contains(data, []byte(fakeSecret)) {
				return fmt.Errorf("%s carried the secret", path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("secrets honors a declared .gitleaksignore and .gitleaks.toml only", func(t *testing.T) {
		ignored := copyFixture(t, shared, "secrets-leaky")
		writeTestFile(t, filepath.Join(ignored, ".gitleaksignore"), "settings.py:generic-api-key:9\n")
		if result := native.Execute(ctx, sourceRequest(t, shared, ignored, CheckSecrets)); result.Status != StatusPassed {
			t.Fatalf("a fingerprint in .gitleaksignore must allowlist its finding: %+v", result)
		}

		req := sourceRequest(t, shared, ignored, CheckSecrets)
		req.Target.Exclude = []string{".gitleaksignore"}
		if result := native.Execute(ctx, req); result.Status != StatusFailed {
			t.Fatalf("an excluded .gitleaksignore must not apply: %+v", result)
		}

		configured := copyFixture(t, shared, "secrets-leaky")
		writeTestFile(t, filepath.Join(configured, ".gitleaks.toml"), "[extend]\nuseDefault = true\n\n[[allowlists]]\ndescription = \"fixture settings\"\npaths = ['''^settings\\.py$''']\n")
		if result := native.Execute(ctx, sourceRequest(t, shared, configured, CheckSecrets)); result.Status != StatusPassed {
			t.Fatalf("a root .gitleaks.toml allowlist must apply: %+v", result)
		}
	})
}
