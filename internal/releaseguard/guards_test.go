package releaseguard

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, root string, success bool, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), args[0], args[1:]...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if (err == nil) != success {
		t.Fatalf("%v: %v\n%s", args, err, output)
	}
	return string(output)
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{
		"scripts/test-rulesets", "scripts/release-on-main", ".github/rulesets/main.json",
		".github/rulesets/proposed/tags.json", ".github/rulesets/proposed/tags-creation.json",
		".github/workflows/verify.yml", ".github/workflows/security.yml",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", path))
		if os.IsNotExist(err) && strings.Contains(path, "/proposed/") {
			data, err = os.ReadFile(filepath.Join("..", "..", strings.Replace(path, "/proposed/", "/", 1)))
		}
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, path), string(data))
	}
	return root
}

func TestReleaseAncestryGuard(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "## [1.2.3] - 2026-10-01\n")
	run(t, root, true, "git", "init", "-b", "main")
	run(t, root, true, "git", "config", "user.email", "fixture@example.invalid")
	run(t, root, true, "git", "config", "user.name", "Release fixture")
	run(t, root, true, "git", "add", ".")
	run(t, root, true, "git", "commit", "-m", "Direct commit without PR, review, or status checks")
	run(t, root, true, "git", "tag", "-a", "v1.2.3", "-m", "Fixture")

	// Main ancestry accepts a direct commit: the guard cannot prove review.
	run(t, root, true, "bash", "scripts/release-on-main", "v1.2.3", "v1.2.3", "main")
	run(t, root, false, "bash", "scripts/release-on-main", "runner/lint/v1.2.3", "HEAD", "main")
	run(t, root, false, "bash", "scripts/release-on-main", "v1.2.3-rc.1", "HEAD", "main")
	run(t, root, false, "bash", "scripts/release-on-main", "v9.9.9", "HEAD", "main")
	run(t, root, false, "bash", "scripts/release-on-main", "v1.2.3", "missing", "main")
	run(t, root, false, "bash", "scripts/release-on-main", "v1.2.3", "HEAD", "missing")

	run(t, root, true, "git", "checkout", "-b", "unmerged")
	writeFile(t, filepath.Join(root, "unmerged"), "outside main\n")
	run(t, root, true, "git", "add", "unmerged")
	run(t, root, true, "git", "commit", "-m", "Unmerged commit")
	output := run(t, root, false, "bash", "scripts/release-on-main", "v1.2.3", "HEAD", "main")
	if !strings.Contains(output, "does not contain") {
		t.Fatalf("missing ancestry rejection: %s", output)
	}
}

func TestReleaseTagPolicies(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"tags.json", "tags-creation.json"} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			root := fixture(t)
			run(t, root, true, "bash", "scripts/test-rulesets")
			path := filepath.Join(root, ".github", "rulesets", "proposed", file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var policy map[string]any
			if err := json.Unmarshal(data, &policy); err != nil {
				t.Fatal(err)
			}

			conditions := policy["conditions"].(map[string]any)["ref_name"].(map[string]any)
			conditions["include"] = []string{"refs/tags/v*"}
			data, err = json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, string(data))
			output := run(t, root, false, "bash", "scripts/test-rulesets")
			if !strings.Contains(output, "both tag namespaces") {
				t.Fatalf("missing namespace rejection: %s", output)
			}
		})
	}
}

func TestProposalsAreExcludedFromLiveComparison(t *testing.T) {
	root := fixture(t)
	bin := filepath.Join(root, "bin")
	writeFile(t, filepath.Join(bin, "gh"), `#!/usr/bin/env bash
set -euo pipefail
case "$2" in
  repos/*/rulesets) echo 1 ;;
  repos/*/rulesets/1) cat "$FIXTURE_ROOT/.github/rulesets/main.json" ;;
  *) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FIXTURE_ROOT", root)

	output := run(t, root, true, "bash", "scripts/test-rulesets", "--live")
	if !strings.Contains(output, "Proposed ruleset") || !strings.Contains(output, "on GitHub matches") {
		t.Fatalf("missing distinction between proposal and snapshot: %s", output)
	}
	// Pretending a proposal is enforced must fail when the API has only main.
	if err := os.Rename(filepath.Join(root, ".github", "rulesets", "proposed", "tags.json"), filepath.Join(root, ".github", "rulesets", "tags.json")); err != nil {
		t.Fatal(err)
	}
	output = run(t, root, false, "bash", "scripts/test-rulesets", "--live")
	if !strings.Contains(output, "committed but not enforced") {
		t.Fatalf("missing unenforced-snapshot rejection: %s", output)
	}
}

func TestReleasePolicyRejectsWeakenedRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		old  string
		new  string
	}{
		{name: "creation", file: "tags-creation.json", old: `"type": "creation"`, new: `"type": "update"`},
		{name: "update", file: "tags.json", old: `"type": "update"`, new: `"type": "creation"`},
		{name: "deletion", file: "tags.json", old: `"type": "deletion"`, new: `"type": "creation"`},
		{name: "force push", file: "tags.json", old: `"type": "non_fast_forward"`, new: `"type": "creation"`},
		{name: "immutable bypass", file: "tags.json", old: `"bypass_actors": []`, new: `"bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}]`},
		{name: "creation role", file: "tags-creation.json", old: `"actor_id": 2`, new: `"actor_id": 4`},
		{name: "excluded tags", file: "tags.json", old: `"exclude": []`, new: `"exclude": ["refs/tags/v1*"]`},
		{name: "disabled", file: "tags.json", old: `"enforcement": "active"`, new: `"enforcement": "disabled"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := fixture(t)
			path := filepath.Join(root, ".github", "rulesets", "proposed", test.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), test.old) {
				t.Fatalf("fixture no longer contains %s", test.old)
			}
			writeFile(t, path, strings.Replace(string(data), test.old, test.new, 1))

			output := run(t, root, false, "bash", "scripts/test-rulesets")
			if !strings.Contains(output, "reviewed bypass policy") {
				t.Fatalf("missing policy rejection: %s", output)
			}
		})
	}
}
