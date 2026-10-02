package verify

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutputBufferRetainsPrefixAndAcceptsRemainder(t *testing.T) {
	t.Parallel()
	buffer := newOutputBuffer()
	prefix := strings.Repeat("x", nativeOutputLimit)
	if n, err := buffer.Write([]byte(prefix)); err != nil || n != len(prefix) || buffer.truncated {
		t.Fatalf("exact limit: n=%d err=%v truncated=%v", n, err, buffer.truncated)
	}
	if n, err := buffer.Write([]byte("discarded")); err != nil || n != 9 || !buffer.truncated || buffer.String() != prefix {
		t.Fatalf("overflow: n=%d err=%v truncated=%v length=%d", n, err, buffer.truncated, len(buffer.String()))
	}
}

func TestMergedToolOutputAndJSONStayBounded(t *testing.T) {
	t.Parallel()
	// NUL has the maximum six-byte JSON string escape expansion.
	output := strings.Repeat("\x00", nativeOutputLimit)
	merged := mergeToolOutput(toolRun{Stdout: output, Stderr: output}, toolRun{Stdout: "tail", Stderr: "tail", ExitCode: 7})
	if len(merged.Stdout) != nativeOutputLimit || len(merged.Stderr) != nativeOutputLimit || len(merged.Warnings) != 2 || merged.ExitCode != 7 {
		t.Fatalf("merged lengths=%d/%d warnings=%v exit=%d", len(merged.Stdout), len(merged.Stderr), merged.Warnings, merged.ExitCode)
	}
	data, err := json.Marshal(Result{Status: StatusPassed, Stdout: merged.Stdout, Stderr: merged.Stderr, Warnings: merged.Warnings})
	if err != nil || len(data) >= recordLimit {
		t.Fatalf("JSON record: length=%d limit=%d err=%v", len(data), recordLimit, err)
	}
}

func TestTruncationWarningRendersInReport(t *testing.T) {
	t.Parallel()
	report := renderFixture()
	report.Results[0].Warnings = []Warning{{Kind: WarningOutputTruncated, Message: "native stdout exceeded 1048576 bytes; retained only the first 1048576 bytes and drained the remainder"}}
	for _, format := range []Format{FormatJSON, FormatText, FormatGitHub, FormatSARIF} {
		text := render(t, report, format, RenderOptions{})
		if !strings.Contains(text, "native stdout exceeded 1048576 bytes") {
			t.Fatalf("%s omitted truncation warning", format)
		}
	}
}

func TestResultWarningsRenderWithoutChangingDiagnosticDetails(t *testing.T) {
	t.Parallel()
	for _, kind := range []WarningKind{WarningRuleModulesSkipped, WarningRuleDeprecated, WarningRuleRenamed} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			warning := Warning{Kind: kind, Message: string(kind) + " migration guidance"}
			report := reportOf([]PlannedCheck{lintCheck("lint", ".")}, Result{ID: "lint", Status: StatusPassed, Warnings: []Warning{warning}})
			for _, format := range []Format{FormatText, FormatGitHub, FormatSARIF} {
				text := render(t, report, format, RenderOptions{})
				if strings.Count(text, warning.Message) != 1 {
					t.Fatalf("%s omitted or duplicated warning: %s", format, text)
				}
			}
			if report.Results[0].Details != nil || report.Results[0].Status != StatusPassed {
				t.Fatal("rendering changed diagnostic details or outcome")
			}
			existing := finding{Code: string(kind), Message: warning.Message, Location: location{File: "a.go", Line: 3}, Advisory: true}
			report.Results[0].Details = findingsDetails([]finding{existing})
			it := findingItems(report)[0]
			if len(it.findings) != 1 || it.findings[0] != existing || !it.located(existing) {
				t.Fatalf("equivalent advisory repeated or lost its source location: %v", it.findings)
			}
		})
	}
}

func TestNativeSkippedRulesWarningHasNoSourceLocation(t *testing.T) {
	t.Parallel()
	check := lintCheck("lint", "services/api")
	check.Environment.Executor = ExecutorNative
	check.RuleModules = []PlannedRuleModule{{Path: "example.com/rules", Version: "v1.0.0", Namespace: "example"}}
	warnings := skippedRuleModules(Request{PlannedCheck: check})
	report := reportOf([]PlannedCheck{check}, Result{ID: check.ID, Status: StatusPassed, Warnings: warnings})

	github := render(t, report, FormatGitHub, RenderOptions{PathPrefix: "repo"})
	if !strings.Contains(github, "::warning title=rule-modules-skipped (lint)::") || strings.Contains(github, "file=") || strings.Contains(github, "line=") {
		t.Fatalf("warning acquired a source location: %s", github)
	}
	text := render(t, report, FormatText, RenderOptions{PathPrefix: "repo"})
	if !strings.Contains(text, "repo/services/api: rule-modules-skipped [advisory]") || strings.Contains(text, "repo/services/api:0:") {
		t.Fatalf("warning acquired a source line: %s", text)
	}
	var sarif sarifLog
	if err := json.Unmarshal([]byte(render(t, report, FormatSARIF, RenderOptions{PathPrefix: "repo"})), &sarif); err != nil {
		t.Fatal(err)
	}
	run := sarif.Runs[0]
	if len(run.Results) != 0 || len(run.Invocations[0].Notifications) != 1 || run.Invocations[0].Notifications[0].Level != sarifWarning || !strings.Contains(run.Invocations[0].Notifications[0].Message.Text, warnings[0].Message) {
		t.Fatalf("module warning became a source result: %+v", run)
	}
}
