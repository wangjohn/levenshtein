package verify

import (
	"slices"
	"strings"
	"testing"
)

// The repository's own configuration declares one check per kind and lets the
// targets list expand it. These are the check IDs each gate ran before that
// rewrite, spelled kind/target, so the literal lists below are the contract.
func TestRepositoryRunsPlanTheSameCheckIDs(t *testing.T) {
	cfg, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}

	goChecks := []string{"go-lint/root", "go-lint/runner", "go-lint/lint", "go-vet/root", "go-vet/runner", "go-vet/lint"}
	for name, want := range map[string][]string{
		"branch":    append(slices.Clone(goChecks), "workflow-lint"),
		"pre-merge": append(slices.Clone(goChecks), "workflow-lint", "self-test"),
		"main":      append(slices.Clone(goChecks), "workflow-lint", "self-test", "go-vuln/root", "go-vuln/runner", "go-vuln/lint"),
	} {
		plan, err := cfg.Plan("../..", name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		var ids []string
		for _, check := range plan.Checks {
			ids = append(ids, check.ID)
			if check.Check.Target == "" || len(check.Check.Targets) != 0 {
				t.Fatalf("%s: %q was planned unexpanded: %+v", name, check.ID, check.Check)
			}
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("%s planned %v, want %v", name, ids, want)
		}
	}
}

const multiTarget = `{"version":1,
"targets":{"root":{"dir":".","inputs":["."]},"runner":{"dir":".","inputs":["."]}},
"environments":{"go":{"executor":"dagger"}},
"checks":{"go-lint":{"kind":"go-lint","targets":["root","runner"],"environment":"go"},"self-test":{"kind":"self-test","target":"root","environment":"go"}},
"runs":{"both":{"checks":["go-lint"]},"one":{"checks":["go-lint/runner"]},"mixed":{"checks":["go-lint/root","self-test"]},%s}}`

func TestTargetsExpandInDeclaredOrder(t *testing.T) {
	cfg, err := Parse([]byte(strings.Replace(multiTarget, "%s", `"unused":{"checks":["self-test"]}`, 1)))
	if err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string][]string{
		"both":  {"go-lint/root", "go-lint/runner"},
		"one":   {"go-lint/runner"},
		"mixed": {"go-lint/root", "self-test"},
	} {
		plan, err := cfg.Plan(t.TempDir(), name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		var ids []string
		for _, check := range plan.Checks {
			ids = append(ids, check.ID)
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("%s planned %v, want %v", name, ids, want)
		}
		if target := plan.Checks[0].Check.Target; !strings.HasSuffix(want[0], target) {
			t.Fatalf("%s bound target %q for %q", name, target, want[0])
		}
	}
}

// Every rejected shape is a way of naming targets that cannot plan: both forms
// at once, neither, an empty or repeated list, a target the check does not
// declare, a per-target reference to a single-target check, and a run that
// selects the same planned check twice.
func TestRejectInvalidTargetSelection(t *testing.T) {
	for name, run := range map[string]string{
		"both forms":      `"broken":{"checks":["both"]}`,
		"neither form":    `"broken":{"checks":["neither"]}`,
		"empty targets":   `"broken":{"checks":["empty"]}`,
		"repeated target": `"broken":{"checks":["repeated"]}`,
		"unknown target":  `"broken":{"checks":["go-lint/missing"]}`,
		"single target":   `"broken":{"checks":["self-test/root"]}`,
		"expanded twice":  `"broken":{"checks":["go-lint","go-lint/root"]}`,
	} {
		data := strings.Replace(multiTarget, "%s", run, 1)
		data = strings.Replace(data, `"self-test":{"kind":"self-test","target":"root","environment":"go"}`,
			`"self-test":{"kind":"self-test","target":"root","environment":"go"},`+
				`"both":{"kind":"go-lint","target":"root","targets":["runner"],"environment":"go"},`+
				`"neither":{"kind":"go-lint","environment":"go"},`+
				`"empty":{"kind":"go-lint","targets":[],"environment":"go"},`+
				`"repeated":{"kind":"go-lint","targets":["root","root"],"environment":"go"}`, 1)

		cfg, err := Parse([]byte(data))
		if err == nil {
			_, err = cfg.Plan(t.TempDir(), "broken")
		}
		if err == nil {
			t.Fatalf("%s: accepted invalid target selection", name)
		}
	}
}
