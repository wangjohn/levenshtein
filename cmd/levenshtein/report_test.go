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
			code, err := runCommand(context.Background(), tc.args, console{in: strings.NewReader(tc.stdin), out: &output})
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
		code, err := runCommand(context.Background(), tc.args, console{in: strings.NewReader(tc.stdin), out: io.Discard})
		if code != 2 || err == nil {
			t.Errorf("%s: code %d, %v", tc.name, code, err)
		}
	}
}
