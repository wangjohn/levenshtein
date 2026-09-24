package gocheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// buildApidiff builds the apidiff that runner/tools pins, as both executors
// do, so the test runs the real tool rather than a recording of it.
func buildApidiff(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "apidiff")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", binary, "golang.org/x/exp/cmd/apidiff")
	cmd.Dir = filepath.Join("..", "..", "tools")
	cmd.Env = testEnv()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the pinned apidiff: %v\n%s", err, output)
	}
	return binary
}

func TestApidiffJudgesTheFixtures(t *testing.T) {
	tool := buildApidiff(t)
	base := filepath.Join(testdata, "apidiff", "base")

	compatible, err := Apidiff(t.Context(), tool, base, filepath.Join(testdata, "apidiff", "compatible"), ".", "off", testEnv())
	if err != nil || len(compatible.Findings) != 0 {
		t.Fatalf("an addition, and breaks confined to internal and main packages, must pass: %+v %v", compatible, err)
	}
	if !slices.Contains(compatible.Notes, "compatible: Circle: added") {
		t.Fatalf("compatible changes must be listed: %v", compatible.Notes)
	}

	breaking, err := Apidiff(t.Context(), tool, base, filepath.Join(testdata, "apidiff", "breaking"), ".", "off", testEnv())
	if err != nil {
		t.Fatalf("breaking must fail for its changes, not error: %v", err)
	}
	var got []string
	for _, finding := range breaking.Findings {
		if finding.Code != CodeApidiff {
			t.Errorf("wrong code: %+v", finding)
		}
		got = append(got, fmt.Sprintf("%s:%d %s", finding.Location.File, finding.Location.Line, finding.Message[strings.LastIndex(finding.Message, ": ")+2:]))
	}
	want := []string{
		"shapes.go:3 removed",
		"shapes.go:11 changed from func() float64 to func() int",
		"units/units.go:9 changed from func(float64) float64 to func(float64, string) float64",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("breaking findings:\n got %v\nwant %v", got, want)
	}
}

func TestApidiffPassesAModuleWithNothingToCompare(t *testing.T) {
	head := filepath.Join(testdata, "apidiff", "breaking")
	missing, err := Apidiff(t.Context(), "levenshtein-unused-apidiff", t.TempDir(), head, ".", "off", testEnv())
	if err != nil || len(missing.Findings) != 0 || !strings.Contains(strings.Join(missing.Notes, "\n"), "did not exist at the base") {
		t.Fatalf("a new module has no API to break: %+v %v", missing, err)
	}

	renamed := t.TempDir()
	writeFiles(t, renamed, map[string]string{"go.mod": "module example.com/apidiff/v2\n\ngo 1.24\n"})
	moved, err := Apidiff(t.Context(), "levenshtein-unused-apidiff", renamed, head, ".", "off", testEnv())
	if err != nil || len(moved.Findings) != 0 || !strings.Contains(strings.Join(moved.Notes, "\n"), "module path changed") {
		t.Fatalf("a new module path has no importers to break: %+v %v", moved, err)
	}
}

func TestParseAPIChanges(t *testing.T) {
	report := strings.Join([]string{
		"! second, different message for obj T, isNew false, part \"\"",
		"  first:  removed",
		"  second: changed",
		"Incompatible changes:",
		"- Scale: removed",
		"- ./units.Convert: changed from func(float64) float64 to func(float64, string) float64",
		"Compatible changes:",
		"- Circle: added",
		"",
	}, "\n")
	changes, err := ParseAPIChanges(report)
	want := []APIChange{
		{Message: "Scale: removed"},
		{Message: "./units.Convert: changed from func(float64) float64 to func(float64, string) float64"},
		{Message: "Circle: added", Compatible: true},
	}
	if err != nil || !slices.Equal(changes, want) {
		t.Fatalf("got %+v %v", changes, err)
	}

	for _, unknown := range []string{"- Scale: removed\n", "Incompatible changes:\nsomething else\n"} {
		if _, err := ParseAPIChanges(unknown); err == nil {
			t.Errorf("%q must be an error, not a pass", unknown)
		}
	}
}

func TestChangeSubjectNamesThePackageAndObject(t *testing.T) {
	for message, want := range map[string][2]string{
		"Scale: removed":                         {"example.com/m", "Scale"},
		"(*Square).Area: removed":                {"example.com/m", "(*Square).Area"},
		"./units.Convert: changed":               {"example.com/m/units", "Convert"},
		"./a/b.T.Field: removed":                 {"example.com/m/a/b", "T.Field"},
		"package example.com/m/old: removed":     {"example.com/m/old", ""},
		"example.com/other.T: changed from x":    {"example.com/other", "T"},
		"./cmd/tool.Run: removed":                {"example.com/m/cmd/tool", "Run"},
		"./v2.x.Y: removed":                      {"example.com/m/v2", "x.Y"},
		"Square, method set of *Square: removed": {"example.com/m", "Square, method set of *Square"},
	} {
		pkg, object := changeSubject(message, "example.com/m")
		if pkg != want[0] || object != want[1] {
			t.Errorf("%q: got (%q, %q), want %v", message, pkg, object, want)
		}
	}
	for object, want := range map[string][2]string{
		"(*Square).Area":                {"Square", "Area"},
		"Square.Area":                   {"Square", "Area"},
		"Scale":                         {"Scale", ""},
		"Square, method set of *Square": {"Square", ""},
		"(*Box[T]).Get":                 {"Box", "Get"},
		"Pair[K, V].Swap":               {"Pair", "Swap"},
		"":                              {"", ""},
	} {
		if name, member := objectNames(object); name != want[0] || member != want[1] {
			t.Errorf("%q: got (%q, %q), want %v", object, name, member, want)
		}
	}
}

func TestDeclarationFallsBackToThePackageAndModule(t *testing.T) {
	head := t.TempDir()
	writeFiles(t, head, map[string]string{
		"lib/go.mod":        "module example.com/lib\n",
		"lib/b.go":          "package lib\n\nfunc (t *T) M() {}\n",
		"lib/a.go":          "// Package lib.\npackage lib\n\ntype T struct{}\n\nvar X, Y = 1, 2\n",
		"lib/a_test.go":     "package lib\n\nfunc Removed() {}\n",
		"lib/sub/sub.go":    "package sub\n",
		"lib/sub/README.md": "not Go\n",
	})
	for _, tc := range []struct {
		pkg    string
		object string
		want   string
	}{
		{"example.com/lib", "(*T).M", "lib/b.go:3"},
		{"example.com/lib", "T.Field", "lib/a.go:4"},
		{"example.com/lib", "Y", "lib/a.go:6"},
		{"example.com/lib", "Removed", "lib/a.go:2"},
		{"example.com/lib/sub", "Gone", "lib/sub/sub.go:1"},
		{"example.com/lib/deleted", "", "lib/go.mod:1"},
		{"example.com/other", "T", "lib/go.mod:1"},
		{"example.com/library", "T", "lib/go.mod:1"},
	} {
		location := declaration(head, "lib", "example.com/lib", tc.pkg, tc.object)
		if got := fmt.Sprintf("%s:%d", location.File, location.Line); got != tc.want {
			t.Errorf("%s %s: got %s, want %s", tc.pkg, tc.object, got, tc.want)
		}
	}
	if _, err := os.Stat(filepath.Join(head, "lib", "a_test.go")); err != nil {
		t.Fatal(err)
	}
}
