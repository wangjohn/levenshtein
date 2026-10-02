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
			existing := finding{Code: string(kind), Message: warning.Message, Location: location{File: "."}, Advisory: true}
			report.Results[0].Details = findingsDetails([]finding{existing})
			if found := findingItems(report)[0].findings; len(found) != 1 {
				t.Fatalf("equivalent advisory repeated: %v", found)
			}
		})
	}
}
