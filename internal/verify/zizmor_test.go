package verify

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

func TestZizmorInputsNameWorkflowsActionsAndDependabot(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yaml",
		".github/workflows/notes.md",
		".github/actions/setup/action.yml",
		".github/actions/nested/deeper/action.yaml",
		".github/actions/setup/README.md",
		".github/dependabot.yml",
		"action.yml",
		"vendor/some/action.yml",
		"runner/testdata/bad/.github/workflows/bad.yml",
	} {
		writeTestFile(t, filepath.Join(source, filepath.FromSlash(path)), "name: x\n")
	}

	inputs, config, err := zizmorInputs(source, []string{"."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/actions/nested/deeper/action.yaml", ".github/actions/setup/action.yml", ".github/dependabot.yml", ".github/workflows/ci.yml", ".github/workflows/release.yaml", "action.yml"}
	if !slices.Equal(inputs, want) || config != "" {
		t.Fatalf("inputs=%v config=%q, want %v and no configuration", inputs, config, want)
	}
	if args := checktool.ZizmorArguments("zizmor", config, inputs); !slices.Contains(args, "--no-config") || !slices.Contains(args, "--offline") {
		t.Fatalf("an unconfigured audit must stay offline and ignore configurations outside the repository: %v", args)
	}

	writeTestFile(t, filepath.Join(source, ".github", "zizmor.yml"), "rules: {}\n")
	if _, config, err := zizmorInputs(source, []string{"."}, nil); err != nil || config != ".github/zizmor.yml" {
		t.Fatalf("the repository's zizmor configuration was not used: %q %v", config, err)
	}
	if args := checktool.ZizmorArguments("zizmor", ".github/zizmor.yml", inputs); !slices.Contains(args, "--config=.github/zizmor.yml") || slices.Contains(args, "--no-config") {
		t.Fatalf("a configured audit must name its file: %v", args)
	}

	writeTestFile(t, filepath.Join(source, "zizmor.yaml"), "rules: {}\n")
	if _, _, err := zizmorInputs(source, []string{"."}, nil); err == nil || !strings.Contains(err.Error(), "configure only one zizmor file") {
		t.Fatalf("two configurations must be an error: %v", err)
	}
}

// The native executor reads only what the Dagger path imports and the
// fingerprint covers, so an undeclared or excluded file can neither change
// the verdict nor leave a cached result stale.
func TestZizmorInputsStayWithinTheTargetInputs(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/actions/setup/action.yml",
		".github/actions/skipped/action.yml",
		"action.yml",
		"zizmor.yml",
	} {
		writeTestFile(t, filepath.Join(source, filepath.FromSlash(path)), "name: x\n")
	}

	inputs, config, err := zizmorInputs(source, []string{".github"}, []string{filepath.Join(".github", "actions", "skipped")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/actions/setup/action.yml", ".github/workflows/ci.yml"}
	if !slices.Equal(inputs, want) || config != "" {
		t.Fatalf("inputs=%v config=%q, want %v and no configuration", inputs, config, want)
	}
}

func TestZizmorInputsRefuseAnEmptyAudit(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "README.md"), "nothing to audit\n")

	if _, _, err := zizmorInputs(source, []string{"."}, nil); err == nil {
		t.Fatal("a repository with nothing to audit must be an error, not a pass")
	}
}

// Host settings must not change what the audit reads or how it runs.
func TestZizmorEnvDropsHostSettingsAndCredentials(t *testing.T) {
	t.Parallel()
	env := zizmorEnv([]string{"PATH=/bin", "ZIZMOR_CONFIG=/elsewhere.yml", "ZIZMOR_OFFLINE=false", "GH_TOKEN=secret", "GITHUB_TOKEN=secret", "GH_HOST=example.com", "HOME=/home/user"})
	if !slices.Equal(env, []string{"PATH=/bin", "HOME=/home/user"}) {
		t.Fatalf("unexpected zizmor environment: %v", env)
	}
}

// zizmor's own GitHub Action runs the same release for the online audits.
func TestZizmorPinMatchesTheAction(t *testing.T) {
	t.Parallel()
	pin, err := readReleasePin("../..", releaseZizmor)
	if err != nil {
		t.Fatal(err)
	}

	workflow, err := os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)zizmorcore/zizmor-action@.*?\n\s+version: (\S+)`).FindSubmatch(workflow)
	if match == nil || string(match[1]) != pin.Version {
		t.Fatalf("security.yml must run zizmor %s like runner/toolchain.json; found %q", pin.Version, match)
	}
}
