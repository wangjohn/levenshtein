package community

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func staleLine(file string, line, column int) string {
	return fmt.Sprintf(`{"code":"staticcheck","severity":"error","location":{"file":%q,"line":%d,"column":%d},"message":%q}`, file, line, column, staleDirective)
}

func TestAdviseMakesAStaleAdvisoryDirectiveAdvisory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "store.go")
	source := "package store\n\nfunc F() {\n\t//lint:ignore errs_sentinel no longer needed\n\t//lint:ignore errs_sentinel,errs_nopanic mixed severities\n}\n"
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	advisory := func(code string) bool { return code == "errs_sentinel" }
	advisoryFinding := `{"code":"errs_sentinel","severity":"warning","location":{"file":"x.go","line":1,"column":1},"message":"compare errors with errors.Is"}`

	for _, test := range []struct {
		name       string
		output     string
		exit       int
		wantExit   int
		severities []string
	}{
		{"advisory finding only", advisoryFinding + "\n", 0, 0, []string{"warning"}},
		{"stale advisory directive", staleLine(file, 4, 2) + "\n", 1, 0, []string{"warning"}},
		{"stale directive naming a failing rule", staleLine(file, 5, 2) + "\n", 1, 1, []string{"error"}},
		{"stale directive off its line", staleLine(file, 3, 2) + "\n", 1, 1, []string{"error"}},
		{"no diagnostics but exit 1 means Staticcheck could not finish", "", 1, 1, nil},
		{"any other exit stands", advisoryFinding + "\n", 2, 2, []string{"warning"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rewritten, exit, err := advise([]byte(test.output), test.exit, advisory)
			if err != nil {
				t.Fatal(err)
			}

			if exit != test.wantExit {
				t.Errorf("exit = %d, want %d", exit, test.wantExit)
			}
			var severities []string
			for line := range strings.Lines(string(rewritten)) {
				var diagnostic jsonDiagnostic
				if err := json.Unmarshal([]byte(line), &diagnostic); err != nil {
					t.Fatal(err)
				}
				severities = append(severities, diagnostic.Severity)
			}
			if strings.Join(severities, ",") != strings.Join(test.severities, ",") {
				t.Errorf("severities = %v, want %v", severities, test.severities)
			}
		})
	}
}

func TestAdviseRefusesOutputThatIsNotJSON(t *testing.T) {
	_, _, err := advise([]byte("store.go:1:1: not json\n"), 1, func(string) bool { return false })

	if err == nil {
		t.Error("text output must be an error, not a pass")
	}
}
