package checktool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sharedTable reads one of the tables in runner/testdata that pin a tool's
// behavior. The runner's own tests and the core linter's read some of them
// too.
func sharedTable[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	var table T
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return table
}

func TestToolExitKeepsOnlyTheDocumentedFailure(t *testing.T) {
	for _, tc := range []struct {
		kind      Kind
		code      int
		output    string
		wantError bool
	}{
		{kind: KindGoVet, code: 1, output: "copylocks: copies lock value"},
		{kind: KindWorkflowLint, code: 1, output: "unknown job"},
		{kind: KindGoVuln, code: 3, output: "reachable vulnerability"},
		{kind: KindGoVuln, code: 1, output: "database unavailable", wantError: true},
		{kind: KindWorkflowLint, code: 3, output: "configuration error", wantError: true},
		{kind: KindGoVet, code: 1, wantError: true},
		{kind: KindGoVet, code: 137, output: "killed", wantError: true},
	} {
		findings, err := ToolExit(tc.kind, "app", Run{ExitCode: tc.code, Stderr: tc.output})
		if (err != nil) != tc.wantError {
			t.Fatalf("%s exit %d: findings=%v error=%v", tc.kind, tc.code, findings, err)
		}
		want := Finding{Code: string(tc.kind), Message: tc.output, Location: Location{File: "app", Line: 1}}
		if !tc.wantError && (len(findings) != 1 || findings[0] != want) {
			t.Fatalf("lost tool diagnostic: %+v, want %+v", findings, want)
		}
	}

	if findings, err := ToolExit(KindGoVet, "app", Run{}); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
	findings, err := ToolExit(KindGoVet, "app", Run{ExitCode: 1, Stdout: " out ", Stderr: "err\n"})
	if err != nil || len(findings) != 1 || findings[0].Message != "out \nerr" {
		t.Fatalf("a finding keeps stdout then stderr: %+v %v", findings, err)
	}
}

// A location outside the scanned root, or one a tool already reported
// relatively, is left as the tool wrote it.
func TestRelativeOnlyRelativizesInsideTheRoot(t *testing.T) {
	for _, tt := range []struct {
		root string
		file string
		want string
	}{
		{root: "/src", file: "/src/pkg/a.go", want: "pkg/a.go"},
		{root: "/src", file: "pkg/a.go", want: "pkg/a.go"},
		{root: "/src", file: "/elsewhere/a.go", want: "/elsewhere/a.go"},
		{root: "/src", file: "/srcother/a.go", want: "/srcother/a.go"},
		{root: "", file: "/src/a.go", want: "/src/a.go"},
	} {
		if got := relative(tt.root, tt.file); got != tt.want {
			t.Errorf("relative(%q, %q) = %q, want %q", tt.root, tt.file, got, tt.want)
		}
	}
}
