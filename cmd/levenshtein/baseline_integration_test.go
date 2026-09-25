//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/verify"
)

const deferBeforeCheck = `package app

import "os"

// %s defers Close before checking the Open error.
func %s(path string) int64 {
	f, err := os.Open(path)
	defer f.Close()
	if err != nil {
		return 0
	}
	return 1
}
`

func writeSource(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// A repository adopts the rules with findings it already has: recording them
// lets the same run pass, a new finding still fails, and fixing a recorded one
// fails until its entry is deleted. This runs the native go-lint check for
// real, so the host needs the pinned Go.
func TestBaselineWithNativeGoLint(t *testing.T) {
	shared, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source := writeConfig(t, `, "baseline": ".levenshtein/baseline.json"`)
	writeSource(t, source, "go.mod", "module example.com/app\n\ngo 1.27.1\n")
	writeSource(t, source, "size.go", strings.ReplaceAll(deferBeforeCheck, "%s", "Size"))
	cache := t.TempDir()
	run := func(extra ...string) (int, string, string) {
		t.Helper()
		var out, notes bytes.Buffer
		args := append([]string{"branch", "--source", source, "--shared", shared, "--cache-dir", cache}, extra...)
		code, err := runCommand(context.Background(), args, console{out: &out, err: &notes})
		if err != nil {
			notes.WriteString(err.Error())
		}
		return code, out.String(), notes.String()
	}

	if code, out, notes := run("--no-baseline", "--format", "text"); code != 1 || !strings.Contains(out, "size.go:8:2: SA5001") {
		t.Fatalf("the fixture must fail without a baseline: %d\n%s\n%s", code, out, notes)
	}

	code, out, notes := run("--write-baseline")
	if code != 0 || !strings.Contains(notes, "wrote ") {
		t.Fatalf("write: %d\n%s\n%s", code, out, notes)
	}
	written, err := os.ReadFile(filepath.Join(source, ".levenshtein", "baseline.json"))
	if err != nil || !strings.Contains(string(written), `"file":"size.go","code":"SA5001"`) {
		t.Fatalf("baseline file: %s %v", written, err)
	}

	code, out, notes = run()
	var report verify.Report
	if code != 0 || json.Unmarshal([]byte(out), &report) != nil || report.Baseline == nil || report.Baseline.Baselined == 0 || report.Status != verify.StatusPassed {
		t.Fatalf("a baselined run must pass: %d\n%s\n%s", code, out, notes)
	}
	if code, _, _ := run(); code != 0 {
		t.Fatal("a repeated run must reach the same verdict")
	}

	// Unrelated lines above the recorded finding move it; it stays baselined.
	writeSource(t, source, "size.go", strings.Replace(strings.ReplaceAll(deferBeforeCheck, "%s", "Size"), "import", "// Package app is a fixture.\n\nimport", 1))
	if code, out, notes := run("--format", "text"); code != 0 {
		t.Fatalf("a moved finding must stay baselined: %d\n%s\n%s", code, out, notes)
	}

	writeSource(t, source, "length.go", strings.ReplaceAll(deferBeforeCheck, "%s", "Length"))
	code, out, notes = run("--format", "text")
	if code != 1 || !strings.Contains(out, "length.go:8:2: SA5001") || strings.Contains(out, "size.go:") {
		t.Fatalf("a new finding must fail and the baselined ones stay hidden: %d\n%s\n%s", code, out, notes)
	}
	if err := os.Remove(filepath.Join(source, "length.go")); err != nil {
		t.Fatal(err)
	}

	writeSource(t, source, "size.go", "package app\n\n// Size is fixed.\nfunc Size(string) int64 { return 1 }\n")
	code, out, notes = run("--format", "text")
	if code != 1 || !strings.Contains(out, ".levenshtein/baseline.json:") || !strings.Contains(out, "baseline-stale") {
		t.Fatalf("a fixed finding must fail until its entry is removed: %d\n%s\n%s", code, out, notes)
	}

	if code, _, notes := run("--write-baseline"); code != 0 || !strings.Contains(notes, "wrote 0 entries") {
		t.Fatalf("rewrite: %d %s", code, notes)
	}
	if code, out, notes := run(); code != 0 {
		t.Fatalf("an emptied baseline must pass a clean run: %d\n%s\n%s", code, out, notes)
	}
}
