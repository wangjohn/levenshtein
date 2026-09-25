package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// savedReport is a report as a run writes it: one go-lint check with one
// finding, which a JSON file or stdin hands back to --render.
const savedReport = `{
  "version": 1,
  "run": "branch",
  "status": "failed",
  "plan": {"version": 1, "run": "branch", "rerun_checks": false, "source": "/src", "checks": [
    {"id": "lint", "check": {"kind": "go-lint", "target": "app", "environment": "host"}, "target": {"dir": ".", "workspace": ".", "inputs": ["."]}, "environment": {"executor": "native"}}
  ]},
  "results": [
    {"id": "lint", "status": "failed", "duration_ms": 1, "verified_at": "2026-09-24T00:00:00Z", "error": "Go policy lint failed", "cache": {"status": "miss", "lookup_ms": 0}, "execution_ms": 1,
     "details": {"findings": [{"code": "LV1005", "message": "file is not gofmt-formatted", "location": {"file": "a.go", "line": 1, "column": 1}}]}}
  ]
}
`

func TestRenderSavedReport(t *testing.T) {
	t.Setenv("PATH", "")
	file := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(file, []byte(savedReport), 0600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"text from a file", []string{"--render", file, "--format", "text"}, "", "a.go:1:1: LV1005 file is not gofmt-formatted\n    hint: run gofmt -w a.go"},
		{"github from stdin", []string{"--render", "-", "--format=github", "--path-prefix", "app"}, savedReport, "::error file=app/a.go,line=1,col=1,title=LV1005 (lint)::file is not gofmt-formatted%0A%0Ahint: run gofmt -w a.go"},
		{"sarif", []string{"--render", file, "--format", "sarif"}, "", `"ruleId": "LV1005"`},
		{"json keeps the report and adds hints", []string{"--render", file}, "", `"hint": "run gofmt -w a.go`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			code, err := runCommand(context.Background(), tc.args, console{in: strings.NewReader(tc.stdin), out: &output, err: io.Discard})
			if code != 0 || err != nil {
				t.Fatalf("rendering a failed report is not itself a failure: code %d, %v", code, err)
			}
			if !strings.Contains(output.String(), tc.want) {
				t.Fatalf("output missing %q:\n%s", tc.want, &output)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{"missing file", []string{"--render", filepath.Join(t.TempDir(), "absent.json")}, ""},
		{"not JSON", []string{"--render", "-"}, "not a report"},
		{"another version", []string{"--render", "-"}, `{"version": 2}`},
	} {
		code, err := runCommand(context.Background(), tc.args, console{in: strings.NewReader(tc.stdin), out: io.Discard, err: io.Discard})
		if code != 2 || err == nil {
			t.Errorf("%s: code %d, %v", tc.name, code, err)
		}
	}
}

// writeConfig writes a one-check native go-lint configuration, with extra
// top-level fields spliced in, and returns the source directory.
func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	source := t.TempDir()
	body := `{"version": 1, "targets": {"app": {"dir": ".", "inputs": ["."]}}, "environments": {"host": {"executor": "native"}},
  "checks": {"lint": {"kind": "go-lint", "target": "app", "environment": "host"}}, "runs": {"branch": {"checks": ["lint"]}}` + extra + `}`
	if err := os.WriteFile(filepath.Join(source, "levenshtein.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return source
}

// Baseline configuration is read before any executor starts, so a broken or
// missing setup exits 2 on a host with no tools at all.
func TestBaselineConfigurationErrorsNeedNoExecutor(t *testing.T) {
	t.Setenv("LEVENSHTEIN_SHARED_ROOT", "")
	t.Setenv("PATH", "")
	quiet := console{out: io.Discard, err: io.Discard}

	unconfigured := writeConfig(t, "")
	if code, err := runCommand(context.Background(), []string{"--source", unconfigured, "--write-baseline"}, quiet); code != 2 || err == nil || !strings.Contains(err.Error(), `needs "baseline"`) {
		t.Fatalf("write without a configured file: code %d, %v", code, err)
	}

	broken := writeConfig(t, `, "baseline": ".levenshtein/baseline.json"`)
	if err := os.MkdirAll(filepath.Join(broken, ".levenshtein"), 0755); err != nil {
		t.Fatal(err)
	}
	entry := `{"kind": "go-vet", "dir": ".", "file": "a.go", "code": "go-vet", "message": "m", "count": 1}`
	if err := os.WriteFile(filepath.Join(broken, ".levenshtein", "baseline.json"), []byte(`{"version": 1, "findings": [`+entry+`]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--source", broken}, {"--source", broken, "--write-baseline"}} {
		if code, err := runCommand(context.Background(), args, quiet); code != 2 || err == nil || !strings.Contains(err.Error(), "cannot be baselined") {
			t.Fatalf("%v: code %d, %v", args, code, err)
		}
	}

	// --no-baseline never reads the file, so only the missing shared checkout stops it.
	if code, err := runCommand(context.Background(), []string{"--source", broken, "--no-baseline"}, quiet); code != 2 || err == nil || !strings.Contains(err.Error(), "set --shared") {
		t.Fatalf("--no-baseline: code %d, %v", code, err)
	}

	escaping := writeConfig(t, `, "baseline": "../baseline.json"`)
	if code, err := runCommand(context.Background(), []string{"--source", escaping, "--dry-run"}, quiet); code != 2 || err == nil || !strings.Contains(err.Error(), "baseline file") {
		t.Fatalf("escaping baseline path: code %d, %v", code, err)
	}
}
