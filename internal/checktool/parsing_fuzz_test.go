package checktool

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzDiagnosticOutput(f *testing.F) {
	for _, seed := range []string{`{"findings":[],"notes":[]}`, `{"findings":[]} trailing`, `{"findings":[]} {"findings":[{}]}`, `{"findings":[{"code":"go-imports","message":"bad","location":{"file":"x.go","line":1}}]}`, `{"code":"SA1000","message":"bad","location":{"file":"x.go","line":1}}`, `{"comments":[]}`, `{"comments":[{"file":"run.sh","line":1,"column":1,"code":2164,"message":"bad"}]}`, `{"comments":[`, `null`, `{}`, `{"notes":["ok"]}`, `{"findings":null}`, `{"comments":null}`, ""} {
		for _, exit := range []int{0, 1, 2, -1} {
			f.Add(seed, "", exit)
		}
	}
	f.Fuzz(func(t *testing.T, stdout, stderr string, exit int) {
		if len(stdout)+len(stderr) > 16<<10 {
			t.Skip()
		}
		run := Run{Stdout: stdout, Stderr: stderr, ExitCode: exit}
		lint, lintErr := LintFindings(run, []string{"all"}, "/source")
		gocheck, _, gocheckErr := GocheckReport(KindGoImports, run)
		shell, shellErr := ShellFindings(run)
		for _, result := range []struct {
			findings []Finding
			err      error
		}{{lint, lintErr}, {gocheck, gocheckErr}, {shell, shellErr}} {
			if result.err == nil {
				if exit != 0 && exit != 1 {
					t.Fatal("tool error accepted as verdict")
				}
				if (exit == 0) != (len(result.findings) == 0) {
					t.Fatal("exit contradicts findings")
				}
				for _, found := range result.findings {
					if found.Code == "" || found.Message == "" || found.Location.File == "" || found.Location.Line < 1 {
						t.Fatal("invalid finding accepted")
					}
				}
			}
		}
		if strings.TrimSpace(stderr) != "" && (lintErr == nil || gocheckErr == nil) {
			t.Fatal("tool stderr accepted as clean verdict")
		}
		var object map[string]json.RawMessage
		if gocheckErr == nil || shellErr == nil {
			if err := json.Unmarshal([]byte(stdout), &object); err != nil || object == nil {
				t.Fatal("accepted a non-object report")
			}
		}
		for _, result := range []struct {
			field string
			err   error
		}{{"findings", gocheckErr}, {"comments", shellErr}} {
			if result.err == nil {
				array := false
				for field, raw := range object {
					var entries []json.RawMessage
					if strings.EqualFold(field, result.field) && json.Unmarshal(raw, &entries) == nil && entries != nil {
						array = true
					}
				}
				if !array {
					t.Fatalf("accepted report without a %s array", result.field)
				}
			}
		}
		if gocheckErr == nil {
			run.Stdout += "\n{}"
			if _, _, err := GocheckReport(KindGoImports, run); err == nil {
				t.Fatal("accepted trailing report")
			}
		}
	})
}
