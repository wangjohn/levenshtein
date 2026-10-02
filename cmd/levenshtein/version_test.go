package main

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/wangjohn/levenshtein/internal/verify"
	"strings"
	"testing"
)

func TestVersionExitsBeforeSetup(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("LEVENSHTEIN_SHARED_ROOT", "")
	var output bytes.Buffer
	code, err := runCommand(t.Context(), []string{"--version", "--source", "/does-not-exist", "--shared", "/does-not-exist", "--format", "invalid", "--jobs", "-1"}, console{out: &output})
	if err != nil || code != 0 || !strings.HasPrefix(output.String(), "levenshtein development commit=") || !strings.Contains(output.String(), " state=") {
		t.Fatalf("version: code=%d err=%v output=%q", code, err, output.String())
	}
	if code, err := runCommand(t.Context(), []string{"--version"}, console{out: failingOutput{}}); code != 2 || err == nil {
		t.Fatalf("lost version write error: code=%d error=%v", code, err)
	}
}

func TestCommandReportIncludesBuild(t *testing.T) {
	t.Parallel()
	source := commandConfig(t, `["/bin/sh", "-c", "exit 0"]`, "")
	var output bytes.Buffer

	code, err := runCommand(t.Context(), []string{"--source", source, "--shared", sharedCheckout(t), "--cache-dir", t.TempDir()}, console{out: &output, err: io.Discard})
	if err != nil || code != 0 {
		t.Fatalf("command: code=%d err=%v", code, err)
	}
	var report verify.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || report.Build == nil || report.Build.Version != "development" || report.Build.GoVersion == "" || report.Results[0].Implementation == nil {
		t.Fatalf("build missing from report: %+v", report)
	}
}
