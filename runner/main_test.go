package main

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"dagger/levenshtein/internal/checktool"
)

// The shipped selection keeps its opt-in and deselected rules off and the rest
// on, and a check can still add an opt-in rule. checktool's own tests cover
// the filter's general cases.
func TestShippedSelectionLeavesOptInRulesOff(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		checks []string
		code   string
		want   bool
	}{
		{"the shipped default keeps the deselected style rules off", tools.Checks, "ST1000", false},
		{"the shipped default keeps gocognit off", tools.Checks, "gocognit", false},
		{"adding gocognit to the shipped default turns it on", append(slices.Clone(tools.Checks), "gocognit"), "gocognit", true},
		{"the shipped default keeps deferInLoop off", tools.Checks, "deferInLoop", false},
		{"adding deferInLoop to the shipped default turns it on", append(slices.Clone(tools.Checks), "deferInLoop"), "deferInLoop", true},
		{"the shipped default keeps everything else on", tools.Checks, "errcheck", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checktool.Allowed(tc.checks, tc.code); got != tc.want {
				t.Fatalf("Allowed(%v, %q) = %v, want %v", tc.checks, tc.code, got, tc.want)
			}
		})
	}
}

func TestSelfTestCoversEveryDefaultRule(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}

	for _, code := range expectedBadCodes {
		if !checktool.Allowed(tools.Checks, code) {
			t.Errorf("the bad fixture expects %s, which the default selection does not report", code)
		}
	}

	script, err := os.ReadFile("../scripts/test-checks")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(script), "\nexpected='")
	block, _, closed := strings.Cut(rest, "'")
	if !found || !closed {
		t.Fatal("scripts/test-checks must define the bad-fixture codes as expected='...'")
	}
	if got := strings.Fields(block); !slices.Equal(got, expectedBadCodes) {
		t.Errorf("scripts/test-checks expects %v, runner expects %v", got, expectedBadCodes)
	}
}

// An added pattern is joined into one -checks flag, so anything but a single
// pattern is refused before the linter runs. The CLI validates levenshtein.json
// with a copy of the same expression.
func TestAddedCheckPatternSyntax(t *testing.T) {
	for _, check := range []string{"gocognit", "-unparam", "SA5*", "S*", "all", "*", "-ST1000", "appendAssign"} {
		if !lintPattern.MatchString(check) {
			t.Errorf("rejected %q", check)
		}
	}
	for _, check := range []string{"", "-", "--unparam", "gocognit,unparam", "SA 5001", " gocognit", "SA*5", "**", "go-cognit", "1000"} {
		if lintPattern.MatchString(check) {
			t.Errorf("accepted %q", check)
		}
	}
}

// Every version the repository records in more than one place names the same
// thing in each, so bumping one copy without the others fails here instead of
// building or running a mix. The paths are relative to runner/.
func TestPinsAgree(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}
	dagger := strings.TrimSpace(readPinFile(t, "../.dagger-version"))
	sdkImage := regexp.MustCompile(`(?m)^GO_IMAGE = "([^"]*)"$`).FindStringSubmatch(readPinFile(t, "../sdk/patched-go/src/patched_go/__init__.py"))
	if sdkImage == nil {
		t.Fatal("sdk/patched-go/src/patched_go/__init__.py sets no GO_IMAGE")
	}

	for _, pin := range []struct {
		name string
		got  string
		want string
	}{
		{".go-version", strings.TrimSpace(readPinFile(t, "../.go-version")), tools.Go},
		{"goImage's tag", strings.SplitN(strings.TrimPrefix(tools.GoImage, "golang:"), "-", 2)[0], tools.Go},
		{"GO_IMAGE in sdk/patched-go", sdkImage[1], tools.GoImage},
		{"the go directive of go.mod", goDirective(t, "../go.mod"), tools.Go},
		{"the go directive of runner/go.mod", goDirective(t, "go.mod"), tools.Go},
		{"the go directive of runner/lint/go.mod", goDirective(t, "lint/go.mod"), tools.Go},
		{"the go directive of runner/community/go.mod", goDirective(t, "community/go.mod"), tools.Go},
		{"the go directive of runner/tools/go.mod", goDirective(t, "tools/go.mod"), tools.Go},
		{"Staticcheck in runner/lint/go.mod", goRequire(t, "lint/go.mod", "honnef.co/go/tools"), tools.Staticcheck},
		{"Staticcheck in runner/community/go.mod", goRequire(t, "community/go.mod", "honnef.co/go/tools"), tools.Staticcheck},
		{"x/tools in runner/community/go.mod", goRequire(t, "community/go.mod", "golang.org/x/tools"), goRequire(t, "lint/go.mod", "golang.org/x/tools")},
		{"engineVersion in dagger.json", readEngineVersion(t, "../dagger.json"), "v" + dagger},
		{"engineVersion in sdk/patched-go/dagger.json", readEngineVersion(t, "../sdk/patched-go/dagger.json"), "v" + dagger},
		{"dagger.io/dagger in go.mod", goRequire(t, "../go.mod", "dagger.io/dagger"), "v" + dagger},
	} {
		if pin.got != pin.want {
			t.Errorf("%s is %q, want %q", pin.name, pin.got, pin.want)
		}
	}

	for line := range strings.SplitSeq(strings.TrimSpace(readPinFile(t, "../scripts/dagger-checksums.txt")), "\n") {
		if !strings.Contains(line, "dagger_v"+dagger+"_") {
			t.Errorf("scripts/dagger-checksums.txt must name only Dagger %s archives: %q", dagger, line)
		}
	}
}

// runner/tools builds several unrelated tools from one module, so a bump to
// one can raise another through a shared dependency. Each tool's module is
// pinned in toolchain.json as well, and must not move unless that pin moves.
func TestToolsModuleKeepsEveryToolAtItsPin(t *testing.T) {
	var pinned struct {
		Tools struct {
			Modules map[string]string `json:"modules"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(toolchainJSON, &pinned); err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)\ntool \((.*?)\)`).FindStringSubmatch(readPinFile(t, "tools/go.mod"))
	if block == nil || len(pinned.Tools.Modules) == 0 {
		t.Fatal("runner/tools/go.mod needs a tool block and toolchain.json a tools object")
	}

	var built []string
	for tool := range strings.FieldsSeq(block[1]) {
		module := ""
		for path := range pinned.Tools.Modules {
			if tool == path || strings.HasPrefix(tool, path+"/") {
				module = path
			}
		}
		if module == "" {
			t.Errorf("runner/tools/go.mod builds %s, whose module toolchain.json does not pin", tool)
			continue
		}
		built = append(built, module)
	}
	for path, version := range pinned.Tools.Modules {
		if !slices.Contains(built, path) {
			t.Errorf("toolchain.json pins %s, which runner/tools/go.mod builds no tool from", path)
		}
		if got := goRequire(t, "tools/go.mod", path); got != version {
			t.Errorf("runner/tools/go.mod requires %s %s, but toolchain.json pins %s", path, got, version)
		}
	}
}

func readPinFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// goDirective is a go.mod file's go version.
func goDirective(t *testing.T, file string) string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^go (\S+)$`).FindStringSubmatch(readPinFile(t, file))
	if match == nil {
		t.Fatalf("%s has no go directive", file)
	}
	return match[1]
}

// goRequire is the version a go.mod file requires of one module, directly or
// indirectly.
func goRequire(t *testing.T, file, module string) string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^(?:require )?\s*` + regexp.QuoteMeta(module) + ` (\S+)`).FindStringSubmatch(readPinFile(t, file))
	if match == nil {
		t.Fatalf("%s does not require %s", file, module)
	}
	return match[1]
}

func readEngineVersion(t *testing.T, file string) string {
	t.Helper()
	var config struct {
		EngineVersion string `json:"engineVersion"`
	}
	if err := json.Unmarshal([]byte(readPinFile(t, file)), &config); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return config.EngineVersion
}
