package checktool

import (
	"strings"
	"testing"
)

// gocheckCase is one row of runner/testdata/gocheck-reports.json.
type gocheckCase struct {
	Name     string      `json:"name"`
	Exit     int         `json:"exit"`
	Stdout   string      `json:"stdout"`
	Stderr   string      `json:"stderr"`
	Want     testVerdict `json:"want"`
	Findings int         `json:"findings"`
	Notes    int         `json:"notes"`
	Contains string      `json:"contains"`
}

// Only a report that matches its own exit code passes or fails, and
// everything else is an error.
func TestGocheckReportRefusesAnythingButACleanReport(t *testing.T) {
	table := sharedTable[struct {
		Cases []gocheckCase `json:"cases"`
	}](t, "gocheck-reports.json")
	if len(table.Cases) == 0 {
		t.Fatal("runner/testdata/gocheck-reports.json has no cases")
	}

	for _, tc := range table.Cases {
		findings, notes, err := GocheckReport(KindGoImports, Run{ExitCode: tc.Exit, Stdout: tc.Stdout, Stderr: tc.Stderr})
		switch tc.Want {
		case verdictPass:
			if err != nil || len(findings) != 0 || len(notes) != tc.Notes {
				t.Errorf("%s: want a pass with %d notes: findings=%v notes=%v error=%v", tc.Name, tc.Notes, findings, notes, err)
			}
		case verdictFinding:
			if err != nil || len(findings) != tc.Findings {
				t.Errorf("%s: want %d findings: findings=%v error=%v", tc.Name, tc.Findings, findings, err)
			}
		case verdictError:
			if err == nil || !strings.Contains(err.Error(), tc.Contains) {
				t.Errorf("%s: want an error containing %q: findings=%v error=%v", tc.Name, tc.Contains, findings, err)
			}
		default:
			t.Errorf("%s: unknown want %q", tc.Name, tc.Want)
		}
	}
}
